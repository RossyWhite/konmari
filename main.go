package main

import (
	"flag"
	"fmt"
	"strings"
	"time"

	//v1 "k8s.io/api/core/v1"

	"os"
	"path/filepath"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	debug = flag.Bool("debug", false, "")
	ns    = flag.String("n", "default", "")
)

func main() {
	flag.Parse()

	var err error
	var config *rest.Config

	if *debug {
		home, _ := os.UserHomeDir()
		config, err = clientcmd.BuildConfigFromFlags("", filepath.Join(home, ".kube", "config"))
	} else {
		config, err = rest.InClusterConfig()
	}

	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	configMaps, err := clientset.CoreV1().ConfigMaps("default").List(metav1.ListOptions{})
	if err != nil {
		panic(err)
	}

	// old configmaps
	var oldConfigMaps []apiv1.ConfigMap
	for _, v := range configMaps.Items {
		t := v.GetObjectMeta().GetCreationTimestamp()
		if ok := t.Time.Before(time.Now().Add(-time.Duration(1) * time.Hour)); ok {
			oldConfigMaps = append(oldConfigMaps, v)
		}
	}

	// pods
	pods, err := clientset.CoreV1().Pods(*ns).List(metav1.ListOptions{})
	if err != nil {
		panic(err)
	}

	for _, cm := range oldConfigMaps {
		for _, pod := range pods.Items {
			if configMapReferencedByPod(&pod, &cm) {
				fmt.Printf("found %s\n", cm.Name)
				break
			}
		}
		fmt.Printf("not found %s\n", cm.Name)
		err = clientset.CoreV1().ConfigMaps("default").Delete(cm.Name, &metav1.DeleteOptions{
			DryRun: []string{metav1.DryRunAll},
		})
		if err != nil {
			panic(err)
		}
	}
}


func configMapReferencedByPod(pod *apiv1.Pod, cm *apiv1.ConfigMap) bool {
	return strings.Contains(pod.String(), cm.Name)
}
