package bfe

import (
	"time"
	"os"
	"os/signal"
	"syscall"
)

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
	"k8s.io/apimachinery/pkg/version"
)

import (
	"github.com/baidu/ingress-bfe/internal/controller"
)

const (
	defaultQPS = 1e6
	defaultBurst = 1e6
)


func main() {
	cfg := parseFlags()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	kubeClient, err := createApiserverClient()
	if err != nil {
		klog.Exitf("Could not establish a connection to the Kubernetes API Server. err:%v", err)
	}

	restClient := kubeClient.NetworkingV1beta1().RESTClient()
	c := controller.NewBfeController(kubeClient, restClient, cfg)

	stopCh := make(chan struct{}, 1)
	go c.Run(stopCh)

	<-signalChan
	c.Exit()
}


func createApiserverClient() (*kubernetes.Clientset, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", "")
	if err != nil {
		return nil, err
	}

	cfg.QPS = defaultQPS
	cfg.Burst = defaultBurst
	cfg.ContentType = "application/vnd.kubernetes.protobuf"

	klog.Infof("Creating API client for %s", cfg.Host)

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	defaultRetry := wait.Backoff{
		Steps:    10,
		Duration: 1 * time.Second,
		Factor:   1.5,
		Jitter:   0.1,
	}

	var v *version.Info
	var retries int
	var lastErr error

	err = wait.ExponentialBackoff(defaultRetry, func() (bool, error) {
		v, err = client.Discovery().ServerVersion()
		if err == nil {
			return true, nil
		}

		lastErr = err
		klog.Infof("Running in Kubernetes Cluster version v%v.%v (%v) - git (%v) commit %v - platform %v",
			v.Major, v.Minor, v.GitVersion, v.GitTreeState, v.GitCommit, v.Platform)
		retries++
		return false, nil
	})
	if err != nil {
		return nil, lastErr
	}

	if retries > 0 {
		klog.Warningf("Initial connection to the Kubernetes API server was retried %d times.", retries)
	}

	return client, nil
}
