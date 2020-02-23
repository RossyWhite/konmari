package main

import (
	"flag"
	"os"
	"strings"
	"sync"
	"time"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
)

var (
	namespace  = flag.String("namespace", "default", "Namespace in which konmari run.")
	age        = flag.Duration("age", 24*time.Hour*30, "Age to judge as old ConfigMap")
	kubeconfig = flag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information.")
	dryrun     = flag.Bool("dryrun", false, "Whether or not delete resource actually.")
)

func main() {
	flag.Parse()

	confpath := os.Getenv("KUBECONFIG")
	if *kubeconfig != "" {
		confpath = *kubeconfig
	}

	config, err := clientcmd.BuildConfigFromFlags("", confpath)
	if err != nil {
		klog.Fatalln(err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalln(err)
	}

	var oldCmItems []apiv1.ConfigMap
	var podItems []apiv1.Pod

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		cmList, err := clientset.CoreV1().ConfigMaps(*namespace).List(metav1.ListOptions{})
		if err != nil {
			klog.Fatalln(err)
		}
		oldCmItems = takeOldCMs(*age, cmList.Items)
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


func parseDryRun(dryrun bool) []string {
	if dryrun {
		return []string{metav1.DryRunAll}
	} else {
		return []string{}
	}
}

func deleteUnreferencedCMs(cli *kubernetes.Clientset, cmitems []apiv1.ConfigMap, podItems []apiv1.Pod) {
	wg := &sync.WaitGroup{}
	for cm := range takeOrphanCMs(cmitems, podItems) {
		wg.Add(1)
		go func(name string) {
			_ = cli.CoreV1().ConfigMaps(*namespace).Delete(name, &metav1.DeleteOptions{
				DryRun: parseDryRun(*dryrun),
			})
			wg.Done()
		}(cm.Name)
	}
	wg.Wait()
}