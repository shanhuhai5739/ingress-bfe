package store

import (
	"bytes"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"net"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/zakjan/cert-chain-resolver/certUtil"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

var (
	oidExtensionSubjectAltName = asn1.ObjectIdentifier{2, 5, 29, 17}
	// EnableSSLChainCompletion Autocomplete SSL certificate chains with missing intermediate CA certificates.
	EnableSSLChainCompletion = false
)

const (
	// DefaultSSLDirectory defines the location where the SSL certificates will be generated
	// This directory contains all the SSL certificates that are specified in Ingress rules.
	// The name of each file is <namespace>-<secret name>.pem. The content is the concatenated
	// certificate and key.
	DefaultSSLDirectory = "/etc/ingress-controller/ssl"
	// ReadWriteByUser defines linux permission to read and write files for the owner user
	ReadWriteByUser = 0700
)

//LocalCertStore store ingress certs
type LocalCertStore struct {
	cache.ThreadSafeStore
}

// NewLocalCertStore creates a new SSLCertTracker store
func NewLocalCertStore() *LocalCertStore {
	return &LocalCertStore{
		cache.NewThreadSafeStore(cache.Indexers{}, cache.Indices{}),
	}
}

// ByKey searches for an ingress in the local ingress Store
func (s LocalCertStore) ByKey(key string) (*SSLCert, error) {
	cert, exists := s.Get(key)
	if !exists {
		return nil, fmt.Errorf("local SSL certificate %v was not found", key)
	}
	return cert.(*SSLCert), nil
}

// SSLCert describes a SSL certificate to be used in a server
type SSLCert struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`

	Certificate *x509.Certificate `json:"-"`

	CACertificate []*x509.Certificate `json:"-"`

	// CAFileName contains the path to the file with the root certificate
	CAFileName string `json:"caFileName"`

	// CASHA contains the sha1 of the ca file.
	// This is used to detect changes in the secret that contains certificates
	CASHA string `json:"caSha"`

	// CRLFileName contains the path to the file with the Certificate Revocation List
	CRLFileName string `json:"crlFileName"`
	// CRLSHA contains the sha1 of the pem file.
	CRLSHA string `json:"crlSha"`

	// PemFileName contains the path to the file with the certificate and key concatenated
	PemFileName string `json:"pemFileName"`

	// PemSHA contains the sha1 of the pem file.
	// This is used to detect changes in the secret that contains certificates
	PemSHA string `json:"pemSha"`

	// CN contains all the common names defined in the SSL certificate
	CN []string `json:"cn"`

	// ExpiresTime contains the expiration of this SSL certificate in timestamp format
	ExpireTime time.Time `json:"expires"`

	// Pem encoded certificate and key concatenated
	PemCertKey string `json:"pemCertKey,omitempty"`

	// UID unique identifier of the Kubernetes Secret
	UID string `json:"uid"`
}

// GetObjectKind implements the ObjectKind interface as a noop
func (s SSLCert) GetObjectKind() schema.ObjectKind {
	return schema.EmptyObjectKind
}

// HashInclude defines if a field should be used or not to calculate the hash
func (s SSLCert) HashInclude(field string, v interface{}) (bool, error) {
	return (field != "PemSHA" && field != "CASHA" && field != "ExpireTime"), nil
}

// Equal tests for equality between two SSLCert types
func (s *SSLCert) Equal(s2 *SSLCert) bool {
	if s == s2 {
		return true
	}
	if s == nil || s2 == nil {
		return false
	}
	if s.CASHA != s2.CASHA {
		return false
	}
	if s.PemSHA != s2.PemSHA {
		return false
	}
	if !s.ExpireTime.Equal(s2.ExpireTime) {
		return false
	}
	if s.PemCertKey != s2.PemCertKey {
		return false
	}
	if s.UID != s2.UID {
		return false
	}

	return StringElementsMatch(s.CN, s2.CN)
}

// CreateSSLCert validates cert and key, extracts common names and returns corresponding SSLCert object
func CreateSSLCert(cert, key []byte, uid string) (*SSLCert, error) {
	var pemCertBuffer bytes.Buffer
	pemCertBuffer.Write(cert)

	if EnableSSLChainCompletion {
		data, err := fullChainCert(cert)
		if err != nil {
			klog.Errorf("Error generating certificate chain for Secret: %v", err)
		} else {
			pemCertBuffer.Reset()
			pemCertBuffer.Write(data)
		}
	}

	pemCertBuffer.Write([]byte("\n"))
	pemCertBuffer.Write(key)

	pemBlock, _ := pem.Decode(pemCertBuffer.Bytes())
	if pemBlock == nil {
		return nil, fmt.Errorf("no valid PEM formatted block found")
	}

	if pemBlock.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("no certificate PEM data found, make sure certificate content starts with 'BEGIN CERTIFICATE'")
	}

	pemCert, err := x509.ParseCertificate(pemBlock.Bytes)
	if err != nil {
		return nil, err
	}

	if _, err := tls.X509KeyPair(cert, key); err != nil {
		return nil, fmt.Errorf("certificate and private key does not have a matching public key: %v", err)
	}

	cn := sets.NewString(pemCert.Subject.CommonName)
	for _, dns := range pemCert.DNSNames {
		if !cn.Has(dns) {
			cn.Insert(dns)
		}
	}

	if len(pemCert.Extensions) > 0 {
		klog.V(3).Info("parsing ssl certificate extensions")
		for _, ext := range getExtension(pemCert, oidExtensionSubjectAltName) {
			dns, _, _, err := parseSANExtension(ext.Value)
			if err != nil {
				klog.Warningf("unexpected error parsing certificate extensions: %v", err)
				continue
			}

			for _, dns := range dns {
				if !cn.Has(dns) {
					cn.Insert(dns)
				}
			}
		}
	}

	hasher := sha1.New()
	hasher.Write(pemCert.Raw)

	return &SSLCert{
		Certificate: pemCert,
		PemSHA:      hex.EncodeToString(hasher.Sum(nil)),
		CN:          cn.List(),
		ExpireTime:  pemCert.NotAfter,
		PemCertKey:  pemCertBuffer.String(),
		UID:         uid,
	}, nil
}
func getExtension(c *x509.Certificate, id asn1.ObjectIdentifier) []pkix.Extension {
	var exts []pkix.Extension
	for _, ext := range c.Extensions {
		if ext.Id.Equal(id) {
			exts = append(exts, ext)
		}
	}
	return exts
}

func parseSANExtension(value []byte) (dnsNames, emailAddresses []string, ipAddresses []net.IP, err error) {
	// RFC 5280, 4.2.1.6

	// SubjectAltName ::= GeneralNames
	//
	// GeneralNames ::= SEQUENCE SIZE (1..MAX) OF GeneralName
	//
	// GeneralName ::= CHOICE {
	//      otherName                       [0]     OtherName,
	//      rfc822Name                      [1]     IA5String,
	//      dNSName                         [2]     IA5String,
	//      x400Address                     [3]     ORAddress,
	//      directoryName                   [4]     Name,
	//      ediPartyName                    [5]     EDIPartyName,
	//      uniformResourceIdentifier       [6]     IA5String,
	//      iPAddress                       [7]     OCTET STRING,
	//      registeredID                    [8]     OBJECT IDENTIFIER }
	var seq asn1.RawValue
	var rest []byte
	if rest, err = asn1.Unmarshal(value, &seq); err != nil {
		return
	} else if len(rest) != 0 {
		err = errors.New("x509: trailing data after X.509 extension")
		return
	}
	if !seq.IsCompound || seq.Tag != 16 || seq.Class != 0 {
		err = asn1.StructuralError{Msg: "bad SAN sequence"}
		return
	}

	rest = seq.Bytes
	for len(rest) > 0 {
		var v asn1.RawValue
		rest, err = asn1.Unmarshal(rest, &v)
		if err != nil {
			return
		}
		switch v.Tag {
		case 1:
			emailAddresses = append(emailAddresses, string(v.Bytes))
		case 2:
			dnsNames = append(dnsNames, string(v.Bytes))
		case 7:
			switch len(v.Bytes) {
			case net.IPv4len, net.IPv6len:
				ipAddresses = append(ipAddresses, v.Bytes)
			default:
				err = errors.New("x509: certificate contained IP address of length " + strconv.Itoa(len(v.Bytes)))
				return
			}
		}
	}

	return
}

// fullChainCert checks if a certificate file contains issues in the intermediate CA chain
// Returns a new certificate with the intermediate certificates.
// If the certificate does not contains issues with the chain it return an empty byte array
func fullChainCert(in []byte) ([]byte, error) {
	cert, err := certUtil.DecodeCertificate(in)
	if err != nil {
		return nil, err
	}

	certPool := x509.NewCertPool()
	certPool.AddCert(cert)

	_, err = cert.Verify(x509.VerifyOptions{
		Intermediates: certPool,
	})
	if err == nil {
		return nil, nil
	}

	certs, err := certUtil.FetchCertificateChain(cert)
	if err != nil {
		return nil, err
	}

	return certUtil.EncodeCertificates(certs), nil
}

// CheckCACert validates a byte array containing one or more CA certificate/s
func CheckCACert(caBytes []byte) ([]*x509.Certificate, error) {
	certs := []*x509.Certificate{}

	var block *pem.Block
	for {
		block, caBytes = pem.Decode(caBytes)
		if block == nil {
			break
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	}

	if len(certs) == 0 {
		return nil, fmt.Errorf("error decoding CA certificate/s")
	}

	return certs, nil
}

// SSLCertOnDisk creates a .pem file with content PemCertKey from the given sslCert
// and sets relevant remaining fields of sslCert object
func SSLCertOnDisk(name string, sslCert *SSLCert) (string, error) {
	pemFileName, _ := getPemFileName(name)

	err := ioutil.WriteFile(pemFileName, []byte(sslCert.PemCertKey), ReadWriteByUser)
	if err != nil {
		return "", fmt.Errorf("could not create PEM certificate file %v: %v", pemFileName, err)
	}

	return pemFileName, nil
}

// getPemFileName returns absolute file path and file name of pem cert related to given fullSecretName
func getPemFileName(fullSecretName string) (string, string) {
	pemName := fmt.Sprintf("%v.pem", fullSecretName)
	return fmt.Sprintf("%v/%v", DefaultSSLDirectory, pemName), pemName
}

// ConfigureCACertWithCertAndKey appends ca into existing PEM file consisting of cert and key
// and sets relevant fields in sslCert object
func ConfigureCACertWithCertAndKey(name string, ca []byte, sslCert *SSLCert) error {
	var buffer bytes.Buffer

	_, err := buffer.Write([]byte(sslCert.PemCertKey))
	if err != nil {
		return fmt.Errorf("could not append newline to cert file %v: %v", sslCert.CAFileName, err)
	}

	_, err = buffer.Write([]byte("\n"))
	if err != nil {
		return fmt.Errorf("could not append newline to cert file %v: %v", sslCert.CAFileName, err)
	}

	_, err = buffer.Write(ca)
	if err != nil {
		return fmt.Errorf("could not write ca data to cert file %v: %v", sslCert.CAFileName, err)
	}

	return ioutil.WriteFile(sslCert.CAFileName, buffer.Bytes(), 0644)
}

// ConfigureCRL creates a CRL file and append it into the SSLCert
func ConfigureCRL(name string, crl []byte, sslCert *SSLCert) error {

	crlName := fmt.Sprintf("crl-%v.pem", name)
	crlFileName := fmt.Sprintf("%v/%v", DefaultSSLDirectory, crlName)

	pemCRLBlock, _ := pem.Decode(crl)
	if pemCRLBlock == nil {
		return fmt.Errorf("no valid PEM formatted block found in CRL %v", name)
	}
	// If the first certificate does not start with 'X509 CRL' it's invalid and must not be used.
	if pemCRLBlock.Type != "X509 CRL" {
		return fmt.Errorf("CRL file %v contains invalid data, and must be created only with PEM formatted certificates", name)
	}

	_, err := x509.ParseCRL(pemCRLBlock.Bytes)
	if err != nil {
		return fmt.Errorf(err.Error())
	}

	err = ioutil.WriteFile(crlFileName, crl, 0644)
	if err != nil {
		return fmt.Errorf("could not write CRL file %v: %v", crlFileName, err)
	}

	sslCert.CRLFileName = crlFileName
	sslCert.CRLSHA = SHA1(crlFileName)

	return nil

}

// CreateCACert is similar to CreateSSLCert but it creates instance of SSLCert only based on given ca after
// parsing and validating it
func CreateCACert(ca []byte) (*SSLCert, error) {
	caCert, err := CheckCACert(ca)
	if err != nil {
		return nil, err
	}

	return &SSLCert{
		CACertificate: caCert,
	}, nil
}

// ConfigureCACert is similar to ConfigureCACertWithCertAndKey but it creates a separate file
// for CA cert and writes only ca into it and then sets relevant fields in sslCert
func ConfigureCACert(name string, ca []byte, sslCert *SSLCert) error {
	caName := fmt.Sprintf("ca-%v.pem", name)
	fileName := fmt.Sprintf("%v/%v", DefaultSSLDirectory, caName)

	err := ioutil.WriteFile(fileName, ca, 0644)
	if err != nil {
		return fmt.Errorf("could not write CA file %v: %v", fileName, err)
	}

	sslCert.CAFileName = fileName

	klog.V(3).Infof("Created CA Certificate for Authentication: %v", fileName)

	return nil
}
