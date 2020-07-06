package store

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"strings"

	apiv1 "k8s.io/api/core/v1"
	networking "k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

// syncSecret synchronizes the content of a TLS Secret (certificate(s), secret
// key) with the filesystem. The resulting files can be used by NGINX.
func (s *K8sStore) syncSecret(key string) {
	s.syncSecretMu.Lock()
	defer s.syncSecretMu.Unlock()

	klog.V(3).Infof("Syncing Secret %q", key)

	cert, err := s.getPemCertificate(key)
	if err != nil {
		if !isErrSecretForAuth(err) {
			klog.Warningf("Error obtaining X.509 certificate: %v", err)
		}
		return
	}

	// create certificates and add or update the item in the store
	cur, err := s.GetLocalSSLCert(key)
	if err == nil {
		if cur.Equal(cert) {
			// no need to update
			return
		}
		klog.Infof("Updating Secret %q in the local store", key)
		s.sslStore.Update(key, cert)
		// this update must trigger an update
		// (like an update event from a change in Ingress)
		s.sendDummyEvent()
		return
	}

	klog.Infof("Adding Secret %q to the local store", key)
	s.sslStore.Add(key, cert)
	// this update must trigger an update
	// (like an update event from a change in Ingress)
	s.sendDummyEvent()
}

// getPemCertificate receives a secret, and creates a ingress.SSLCert as return.
// It parses the secret and verifies if it's a keypair, or a 'ca.crt' secret only.
func (s *K8sStore) getPemCertificate(secretName string) (*SSLCert, error) {
	secret, err := s.listers.Secret.ByKey(secretName)
	if err != nil {
		return nil, err
	}

	cert, okcert := secret.Data[apiv1.TLSCertKey]
	key, okkey := secret.Data[apiv1.TLSPrivateKeyKey]
	ca := secret.Data["ca.crt"]

	crl := secret.Data["ca.crl"]

	auth := secret.Data["auth"]

	// namespace/secretName -> namespace-secretName
	nsSecName := strings.Replace(secretName, "/", "-", -1)

	var sslCert *SSLCert
	if okcert && okkey {
		if cert == nil {
			return nil, fmt.Errorf("key 'tls.crt' missing from Secret %q", secretName)
		}

		if key == nil {
			return nil, fmt.Errorf("key 'tls.key' missing from Secret %q", secretName)
		}

		sslCert, err = CreateSSLCert(cert, key, string(secret.UID))
		if err != nil {
			return nil, fmt.Errorf("unexpected error creating SSL Cert: %v", err)
		}

		if len(ca) > 0 {
			caCert, err := CheckCACert(ca)
			if err != nil {
				return nil, fmt.Errorf("parsing CA certificate: %v", err)
			}

			path, err := SSLCertOnDisk(nsSecName, sslCert)
			if err != nil {
				return nil, fmt.Errorf("error while storing certificate and key: %v", err)
			}

			sslCert.PemFileName = path
			sslCert.CACertificate = caCert
			sslCert.CAFileName = path
			sslCert.CASHA = SHA1(path)

			err = ConfigureCACertWithCertAndKey(nsSecName, ca, sslCert)
			if err != nil {
				return nil, fmt.Errorf("error configuring CA certificate: %v", err)
			}

			if len(crl) > 0 {
				err = ConfigureCRL(nsSecName, crl, sslCert)
				if err != nil {
					return nil, fmt.Errorf("error configuring CRL certificate: %v", err)
				}
			}
		}

		msg := fmt.Sprintf("Configuring Secret %q for TLS encryption (CN: %v)", secretName, sslCert.CN)
		if ca != nil {
			msg += " and authentication"
		}

		if crl != nil {
			msg += " and CRL"
		}

		klog.V(3).Info(msg)
	} else if len(ca) > 0 {
		sslCert, err = CreateCACert(ca)
		if err != nil {
			return nil, fmt.Errorf("unexpected error creating SSL Cert: %v", err)
		}

		err = ConfigureCACert(nsSecName, ca, sslCert)
		if err != nil {
			return nil, fmt.Errorf("error configuring CA certificate: %v", err)
		}

		if len(crl) > 0 {
			err = ConfigureCRL(nsSecName, crl, sslCert)
			if err != nil {
				return nil, err
			}
		}
		// makes this secret in 'syncSecret' to be used for Certificate Authentication
		// this does not enable Certificate Authentication
		klog.V(3).Infof("Configuring Secret %q for TLS authentication", secretName)
	} else {
		if auth != nil {
			return nil, ErrSecretForAuth
		}

		return nil, fmt.Errorf("secret %q contains no keypair or CA certificate", secretName)
	}

	sslCert.Name = secret.Name
	sslCert.Namespace = secret.Namespace

	return sslCert, nil
}

// sendDummyEvent sends a dummy event to trigger an update
// This is used in when a secret change
func (s *K8sStore) sendDummyEvent() {
	s.updateCh.In() <- Event{
		Type: UpdateEvent,
		Obj: &networking.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dummy",
				Namespace: "dummy",
			},
		},
	}
}

// SHA1 returns the SHA1 of a file.
func SHA1(filename string) string {
	hasher := sha1.New()
	s, err := ioutil.ReadFile(filename)
	if err != nil {
		klog.Errorf("Error reading file %v", err)
		return ""
	}

	hasher.Write(s)
	return hex.EncodeToString(hasher.Sum(nil))
}

// ErrSecretForAuth error to indicate a secret is used for authentication
var ErrSecretForAuth = fmt.Errorf("secret is used for authentication")

func isErrSecretForAuth(e error) bool {
	return e == ErrSecretForAuth
}
