package controller

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/baidu/ingress-bfe/internal/bfe"
	"github.com/baidu/ingress-bfe/internal/config"
	"github.com/baidu/ingress-bfe/internal/queue"
	"github.com/baidu/ingress-bfe/internal/store"
	"github.com/eapache/channels"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
)

const (
	controllerName = "bfe-ingress-controller"
)

type BfeController struct {
	config          config.Configuration
	kubeClient      kubernetes.Interface
	recorder        record.EventRecorder
	syncQueue       *queue.Queue
	stopCh          chan struct{}
	updateCh        *channels.RingChannel
	store           store.Store
	isShuttiingDown bool
	command         *bfe.Command
	bfeErrCh        chan error
}

func NewBfeController(kubeClient kubernetes.Interface, cfg config.Configuration) (controller *BfeController) {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(cfg.Namespace),
	})
	controller = &BfeController{
		kubeClient: kubeClient,
		config:     cfg,
		recorder: eventBroadcaster.NewRecorder(scheme.Scheme, apiv1.EventSource{
			Component: controllerName,
		}),
		stopCh:   make(chan struct{}),
		updateCh: channels.NewRingChannel(1024),
		command:  bfe.NewCommand(),
	}
	controller.store = store.NewStore(kubeClient, cfg.Namespace, cfg.ResycPeriod, controller.updateCh)

	controller.syncQueue = queue.NewTaskQueue(controller.syncIngress)

	return controller
}

//Run starts a new bfe controller master process running in the foreground
func (b *BfeController) Run() {
	klog.Info("Starting bfe ingress controller")

	b.store.Run(b.stopCh)

	//start bfe process
	cmd := b.command.ExecCommand()
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}
	b.start(cmd)
	go b.syncQueue.Run(time.Second, b.stopCh)

	for {
		select {
		case err := <-b.bfeErrCh:
			if b.isShuttiingDown {
				return
			}
			if bfe.IsRespawnIfRequired(err) {
				return
			}
		case event := <-b.updateCh.Out():
			if b.isShuttiingDown {
				break
			}
			if evt, ok := event.(store.Event); ok {
				klog.V(3).Info("Event %v received - object %v", evt.Type, evt.Obj)
				if evt.Type == store.ConfigurationEvent {
					b.syncQueue.EnqueueTask(queue.GetDummyObject("configmap-change"))
				}
				b.syncQueue.EnqueueTask(evt.Obj)
			} else {
				klog.Warningf("Unexpected event type received %T", event)
			}
		case <-b.stopCh:
			return
		}

	}

}

func (b *BfeController) start(cmd *exec.Cmd) {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		klog.Fatal("bfe start error:%v", err)
		b.bfeErrCh <- err
		return
	}
	go func() {
		b.bfeErrCh <- cmd.Wait()
	}()
}

//Stop gracefully stops the bfe mastere process
func (b *BfeController) Stop() error {
	b.isShuttiingDown = true
	if b.syncQueue.IsShuttingDown() {
		return fmt.Errorf("shutdown already in progress")
	}
	klog.Info("Shutting down controller queues")
	close(b.stopCh)
	go b.syncQueue.Shutdown()

	//send stop signal to bfe
	klog.Info("Stopping bfe process")
	if err := b.command.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	//TODO wait for the bfe exit
	b.command.Cmd.Wait()

	return nil
}

// syncIngress collects all the pieces required to assemble the NGINX
// configuration file and passes the resulting data structures to the backend
// (OnUpdate) when a reload is deemed necessary.
func (n *BfeController) syncIngress(interface{}) error {
	return nil
}
