package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	gamev1alpha1 "github.com/anvil-platform/anvil/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(gamev1alpha1.AddToScheme(scheme))
}

func main() {
	var kubeconfig string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = filepath.Join(home, ".kube", "config")
	} else {
		kubeconfig = os.Getenv("KUBECONFIG")
	}
	flag.StringVar(&kubeconfig, "kubeconfig", kubeconfig, "absolute path to the kubeconfig file")
	
	var numWorlds int
	var namespace string
	var gameDefName string
	var realmName string

	flag.IntVar(&numWorlds, "worlds", 10, "Number of worlds to spawn")
	flag.StringVar(&namespace, "namespace", "default", "Namespace to spawn worlds in")
	flag.StringVar(&gameDefName, "gamedef", "standard-match", "GameDefinition name")
	flag.StringVar(&realmName, "realm", "eu-west", "Realm name")
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		log.Fatalf("Error building kubeconfig: %v", err)
	}

	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		log.Fatalf("Error creating client: %v", err)
	}

	fmt.Printf("Starting load test: %d worlds in namespace %s\n", numWorlds, namespace)

	var wg sync.WaitGroup
	start := time.Now()
	latencies := make(chan time.Duration, numWorlds)

	for i := 0; i < numWorlds; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			worldName := fmt.Sprintf("load-test-world-%d-%d", time.Now().Unix(), id)
			
			world := &gamev1alpha1.WorldInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:      worldName,
					Namespace: namespace,
				},
				Spec: gamev1alpha1.WorldInstanceSpec{
					GameRef: gamev1alpha1.ObjectRef{Name: gameDefName},
					RealmRef: &gamev1alpha1.ObjectRef{Name: realmName},
					WorldID: worldName,
					Region: "eu-west-1",
					ShardCount: 1,
				},
			}

			createStart := time.Now()
			fmt.Printf("Creating world %s\n", worldName)
			if err := k8sClient.Create(context.Background(), world); err != nil {
				fmt.Printf("Error creating world %s: %v\n", worldName, err)
				return
			}

			// Poll for status
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			for {
				select {
				case <-ctx.Done():
					fmt.Printf("Timeout waiting for world %s\n", worldName)
					return
				case <-time.After(1 * time.Second):
					var currentWorld gamev1alpha1.WorldInstance
					if err := k8sClient.Get(ctx, client.ObjectKey{Name: worldName, Namespace: namespace}, &currentWorld); err != nil {
						continue
					}
					if currentWorld.Status.Phase == "Running" {
						latency := time.Since(createStart)
						latencies <- latency
						fmt.Printf("World %s running in %v\n", worldName, latency)
						return
					}
				}
			}
		}(i)
	}

	wg.Wait()
	close(latencies)
	totalDuration := time.Since(start)
	
	var totalLatency time.Duration
	count := 0
	for l := range latencies {
		totalLatency += l
		count++
	}

	if count > 0 {
		avgLatency := totalLatency / time.Duration(count)
		fmt.Printf("Load test completed in %v. Avg startup latency: %v\n", totalDuration, avgLatency)
	} else {
		fmt.Printf("Load test completed in %v. No worlds started successfully.\n", totalDuration)
	}
}
