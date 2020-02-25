package main

import (
	"os"
	"strings"
	"sync"
	"time"

	flag "github.com/spf13/pflag"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
)

var (
	namespace         = flag.String("namespace", "default", "Namespace in which konmari run.")
	deletePeriod      = flag.Duration("deletePeriod", 24*time.Hour*30, "Period to judge as old Object.")
	kubeconfig        = flag.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information.")
	dryrun            = flag.Bool("dryrun", false, "Whether or not to actually delete Objects.")
	disableSecrets    = flag.Bool("disableSecrets", false, "Whether or not to ignore Secrets.")
	disableConfigMaps = flag.Bool("disableConfigMaps", false, "Whether or not to ignore ConfigMaps.")
)

type Options struct {
	Namespace         string
	DeletePeriod      time.Duration
	Kubeconfig        string
	Dryrun            []string
	DisableSecrets    bool
	DisableConfigMaps bool
}

func main() {
	klog.InitFlags(nil)

	flag.Parse()
	opts := createOptions()

	clientset, err := kubernetes.NewForConfig(getKubeConfig(opts.Kubeconfig))
	if err != nil {
		klog.Fatalln(err)
	}

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

	if !opts.DisableSecrets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, err := clientset.CoreV1().Secrets(*namespace).List(metav1.ListOptions{
				FieldSelector: "type=Opaque",
			})
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
				err := clientset.CoreV1().ConfigMaps(*namespace).Delete(r.Name, &metav1.DeleteOptions{
					DryRun: opts.Dryrun,
				})
				if err != nil {
					klog.Errorf("failed to delete %s\n", r.Name)
				}
			}()
		}
	}

	if !opts.DisableSecrets {
		for _, r := range oldSecrets.GetUnreferencedObjects(pods).items {
			wg2.Add(1)
			go func() {
				defer wg2.Done()
				err := clientset.CoreV1().Secrets(*namespace).Delete(r.Name, &metav1.DeleteOptions{
					DryRun: opts.Dryrun,
				})
				if err != nil {
					klog.Errorf("failed to delete %s\n", r.Name)
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
		if !referencedBy(cm.Name, pods) {
			klog.V(2).Infof("deletion candidate found: %s", cm.Name)
			items = append(items, cm)
		}
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
		if !referencedBy(s.Name, pods) {
			klog.V(2).Infof("deletion candidate found: %s", s.Name)
			items = append(items, s)
		}
	}
	return &secretList{items: items}
}

func referencedBy(name string, pods []apiv1.Pod) bool {
	for _, pod := range pods {
		if contain := strings.Contains(pod.String(), name); contain {
			return true
		}
	}
	return false
}

func createOptions() *Options {
	return &Options{
		Namespace:         *namespace,
		DeletePeriod:      *deletePeriod,
		Kubeconfig:        parseKubeconfigFlag(*kubeconfig),
		Dryrun:            parseDryRunFlag(*dryrun),
		DisableSecrets:    *disableSecrets,
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
