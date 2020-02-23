package main

import (
	"flag"
	"os"
	"strings"
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
	//dryrun     = flag.Bool("dryrun", false, "Whether or not delete resource actually.")
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

	cmList, err := clientset.CoreV1().ConfigMaps(*namespace).List(metav1.ListOptions{})
	if err != nil {
		klog.Fatalln(err)
	}
	oldCMs := takeOlderCMs(*age, cmList.Items)

	podList, err := clientset.CoreV1().Pods(*namespace).List(metav1.ListOptions{})
	if err != nil {
		klog.Fatalln(err)
	}

	for cm := range takeOrphanCMs(oldCMs, podList.Items) {
		_ = clientset.CoreV1().ConfigMaps(*namespace).Delete(cm.Name, &metav1.DeleteOptions{
			DryRun: []string{metav1.DryRunAll},
		})
	}
}

func takeOlderCMs(age time.Duration, list []apiv1.ConfigMap) []apiv1.ConfigMap {
	var ret []apiv1.ConfigMap
	for _, v := range list {
		t := v.GetObjectMeta().GetCreationTimestamp()
		if ok := t.Time.Before(time.Now().Add(-age)); ok {
			ret = append(ret, v)
		}
	}

	return ret
}

func takeOrphanCMs(cmList []apiv1.ConfigMap, podList []apiv1.Pod) <-chan apiv1.ConfigMap {
	ch := make(chan apiv1.ConfigMap, len(cmList))

	go func() {
		for _, cm := range cmList {
			for _, pod := range podList {
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

