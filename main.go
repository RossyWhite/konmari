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

func main() {
	flag.Parse()
	opts := createOptions()

	clientset := kubernetes.NewForConfigOrDie(getKubeConfig(opts.Kubeconfig))

	var pods []apiv1.Pod
	var oldConfigMaps *configMapList
	var oldSecrets *secretList

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		podList, err := clientset.CoreV1().Pods(*namespace).List(metav1.ListOptions{})
		if err != nil {
			klog.Fatal(err)
		}
		pods = podList.Items
	}()

	if !opts.DisableConfigMaps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, err := clientset.CoreV1().ConfigMaps(*namespace).List(metav1.ListOptions{})
			if err != nil {
				klog.Fatal(err)
			}
			cmList := &configMapList{items: l.Items}
			oldConfigMaps = cmList.GetOnlyCreatedBefore(opts.DeletePeriod)
		}()

	}

	if !opts.DisableSecret {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, err := clientset.CoreV1().Secrets(*namespace).List(metav1.ListOptions{})
			if err != nil {
				klog.Fatal(err)
			}
			sList := &secretList{items: l.Items}
			oldSecrets = sList.GetOnlyCreatedBefore(opts.DeletePeriod)
		}()

	}

	wg.Wait()

	wg2 := sync.WaitGroup{}
	if !opts.DisableConfigMaps {
		for _, r := range oldConfigMaps.GetUnreferencedObjects(pods).items {
			wg2.Add(1)
			go func() {
				defer wg2.Done()
				err := clientset.CoreV1().ConfigMaps(*namespace).Delete(r.Name, &metav1.DeleteOptions{})
				if err != nil {
					klog.Infof("failed to delete %s\n", r.Name)
				}
			}()
		}
	}

	if !opts.DisableSecret {
		for _, r := range oldSecrets.GetUnreferencedObjects(pods).items {
			wg2.Add(1)
			go func() {
				defer wg2.Done()
				err := clientset.CoreV1().Secrets(*namespace).Delete(r.Name, &metav1.DeleteOptions{})
				if err != nil {
					klog.Infof("failed to delete %s\n", r.Name)
				}
			}()
		}
	}

	wg2.Wait()

}

type DeletableResource interface {
	GetOnlyCreatedBefore(period time.Duration) *DeletableResource
	GetUnreferencedObjects(pods []apiv1.Pod) *DeletableResource
}

type configMapList struct {
	items []apiv1.ConfigMap
}

func (cmList configMapList) GetOnlyCreatedBefore(period time.Duration) *configMapList {
	var ret []apiv1.ConfigMap
	for _, v := range cmList.items {
		t := v.GetObjectMeta().GetCreationTimestamp()
		if ok := t.Time.Before(time.Now().Add(-period)); ok {
			ret = append(ret, v)
		}
	}
	return &configMapList{items: ret}
}

func (cmList configMapList) GetUnreferencedObjects(pods []apiv1.Pod) *configMapList {
	var items []apiv1.ConfigMap
	for _, cm := range cmList.items {
		for _, pod := range pods {
			if referencedBy(cm.Name, &pod) {
				break
			}
		}
		klog.Infof("deletion candidate found: %s", cm.Name)
		items = append(items, cm)
	}
	return &configMapList{items: items}
}

type secretList struct {
	items []apiv1.Secret
}

func (sList secretList) GetOnlyCreatedBefore(period time.Duration) *secretList {
	var ret []apiv1.Secret
	for _, v := range sList.items {
		t := v.GetObjectMeta().GetCreationTimestamp()
		if ok := t.Time.Before(time.Now().Add(-period)); ok {
			ret = append(ret, v)
		}
	}
	return &secretList{items: ret}
}

func (sList secretList) GetUnreferencedObjects(pods []apiv1.Pod) *secretList {
	var items []apiv1.Secret
	for _, s := range sList.items {
		for _, pod := range pods {
			if referencedBy(s.Name, &pod) {
				break
			}
		}
		klog.Infof("deletion candidate found: %s", s.Name)
		items = append(items, s)
	}
	return &secretList{items: items}
}

func referencedBy(name string, pod *apiv1.Pod) bool {
	return strings.Contains(pod.String(), name)
}

//_ = cli.CoreV1().ConfigMaps(*namespace).Delete(name, &metav1.DeleteOptions{})

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
