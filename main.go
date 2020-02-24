package main

import (
	"flag"
	"k8s.io/client-go/rest"
	"os"
	"strings"
	"sync"
	"time"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
)

var (
	namespace         = flag.String("namespace", "default", "Namespace in which konmari run.")
	deletePeriod      = flag.Duration("deletePeriod", 24*time.Hour*30, "Period to judge as old ConfigMap.")
	kubeconfig        = flag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information.")
	dryrun            = flag.Bool("dryrun", false, "Whether or not to delete resource actually.")
	disableSecret     = flag.Bool("disableSecret", false, "Whether or not to disable secret.")
	disableConfigMaps = flag.Bool("disableConfigMaps", false, "Whether or not to disable ConfigMaps.")
)

type Options struct {
	Namespace         string
	DeletePeriod      time.Duration
	Kubeconfig        string
	Dryrun            []string
	DisableSecret     bool
	DisableConfigMaps bool
}

type deletable interface {

}

type ConfigMaps struct {
	cli v1.ConfigMapInterface

}

type Secrets struct {
	cli v1.SecretInterface
}

func main() {
	flag.Parse()
	opts := createOptions()

	clientset := kubernetes.NewForConfigOrDie(getKubeConfig(opts.Kubeconfig))

	for _, r := range  getDeletableTypes(opts, clientset) {
		run()
	}


	var oldCmItems []apiv1.ConfigMap
	var podItems []apiv1.Pod

	os.Exit(1)

	//cmCli := clientset.CoreV1().ConfigMaps(*namespace)
	//if !opts.disableSecret {
	//	secretCli := clientset.CoreV1().Secrets(*namespace)
	//}

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		cmList, err := clientset.CoreV1().ConfigMaps(*namespace).List(metav1.ListOptions{})
		if err != nil {
			klog.Fatalln(err)
		}
		oldCmItems = takeOldCMs(*deletePeriod, cmList.Items)
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		podList, err := clientset.CoreV1().Pods(*namespace).List(metav1.ListOptions{})
		if err != nil {
			klog.Fatalln(err)
		}
		podItems = podList.Items
		wg.Done()
	}()
	wg.Wait()

	deleteUnreferencedCMs(clientset, oldCmItems, podItems)
}

func takeOldCMs(age time.Duration, cmItems []apiv1.ConfigMap) []apiv1.ConfigMap {
	var ret []apiv1.ConfigMap
	for _, v := range cmItems {
		t := v.GetObjectMeta().GetCreationTimestamp()
		if ok := t.Time.Before(time.Now().Add(-age)); ok {
			ret = append(ret, v)
		}
	}

	return ret
}

func takeOrphanCMs(cmItems []apiv1.ConfigMap, podItems []apiv1.Pod) <-chan apiv1.ConfigMap {
	ch := make(chan apiv1.ConfigMap, len(cmItems))

	go func() {
		defer close(ch)
		for _, cm := range cmItems {
			for _, pod := range podItems {
				if referencedBy(&cm, &pod) {
					break
				}
			}
			klog.Infof("deletion candidate found: %s", cm.Name)
			ch <- cm
		}
	}()

	return ch
}

func referencedBy(cm *apiv1.ConfigMap, pod *apiv1.Pod) bool {
	return strings.Contains(pod.String(), cm.Name)
}

func deleteUnreferencedCMs(cli *kubernetes.Clientset, cmitems []apiv1.ConfigMap, podItems []apiv1.Pod) {
	wg := &sync.WaitGroup{}
	for cm := range takeOrphanCMs(cmitems, podItems) {
		wg.Add(1)
		go func(name string) {
			_ = cli.CoreV1().ConfigMaps(*namespace).Delete(name, &metav1.DeleteOptions{})
			wg.Done()
		}(cm.Name)
	}
	wg.Wait()
}

func createOptions() *Options {
	return &Options{
		Namespace:         *namespace,
		DeletePeriod:      *deletePeriod,
		Kubeconfig:        parseKubeconfigFlag(*kubeconfig),
		Dryrun:            parseDryRunFlag(*dryrun),
		DisableSecret:     *disableSecret,
		DisableConfigMaps: *disableConfigMaps,
	}
}

func parseDryRunFlag(dryrun bool) []string {
	if dryrun {
		return []string{metav1.DryRunAll}
	} else {
		return []string{}
	}
}

func parseKubeconfigFlag(kubeconfig string) string {
	if kubeconfig != "" {
		return kubeconfig
	}
	return os.Getenv("KUBECONFIG")
}

func getKubeConfig(path string) *rest.Config {
	config, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		klog.Fatalln(err)
	}
	return config
}

func getDeletableTypes(opts *Options, clientset *kubernetes.Clientset) []deletable {
	var deletables []deletable
	if !opts.DisableConfigMaps {
		deletables = append(deletables, &ConfigMaps{
			cli: clientset.CoreV1().ConfigMaps(opts.Namespace),
		})
	}

	if !opts.DisableSecret {
		deletables = append(deletables, &Secrets{
			cli: clientset.CoreV1().Secrets(opts.Namespace),
		})
	}

	return deletables
}

func run() {

}