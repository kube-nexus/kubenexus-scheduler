/*
Copyright 2026 The KubeNexus Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package e2e contains end-to-end tests for KubeNexus scheduler.
// These tests run against a real Kubernetes cluster (Kind or existing cluster).
//
// Requirements:
// - Kind installed: go install sigs.k8s.io/kind@latest
// - kubectl installed and in PATH
// - Docker running
//
// Run with: make test-e2e
package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	clientset      *kubernetes.Clientset
	clusterCreated bool
)

// TestMain sets up Kind cluster before tests and tears it down after
func TestMain(m *testing.M) {
	// Check if we should use an existing cluster
	if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
		fmt.Println("Using existing Kubernetes cluster")
		setupClient()
		code := m.Run()
		os.Exit(code)
	}

	// Create Kind cluster for testing
	fmt.Println("Creating Kind cluster for E2E tests...")
	if err := createKindCluster(); err != nil {
		fmt.Printf("Failed to create Kind cluster: %v\n", err)
		os.Exit(1)
	}
	clusterCreated = true

	// Setup Kubernetes client
	setupClient()

	// Deploy KubeNexus scheduler
	fmt.Println("Deploying KubeNexus scheduler...")
	if err := deployScheduler(); err != nil {
		fmt.Printf("Failed to deploy scheduler: %v\n", err)
		cleanupKindCluster()
		os.Exit(1)
	}

	// Wait for scheduler to be ready
	if err := waitForSchedulerReady(); err != nil {
		fmt.Printf("Scheduler not ready: %v\n", err)
		cleanupKindCluster()
		os.Exit(1)
	}

	// Run tests
	code := m.Run()

	// Cleanup
	if clusterCreated {
		fmt.Println("Cleaning up Kind cluster...")
		cleanupKindCluster()
	}

	os.Exit(code)
}

// TestE2EGangScheduling tests gang scheduling on a real cluster
func TestE2EGangScheduling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	ctx := context.Background()
	namespace := "test-gang-" + time.Now().Format("20060102-150405")

	// Create test namespace
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	if _, err := clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() { _ = clientset.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{}) }() //nolint:errcheck // Cleanup errors are intentionally ignored

	// Create a job with gang scheduling
	job := makeGangJob("distributed-training", namespace, 4)
	if _, err := clientset.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Wait for all pods to be scheduled
	err := wait.PollUntilContextTimeout(ctx, 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "job-name=distributed-training",
		})
		if err != nil {
			return false, err
		}

		if len(pods.Items) != 4 {
			t.Logf("Waiting for all 4 pods to be created, got %d", len(pods.Items))
			return false, nil
		}

		scheduledCount := 0
		for _, pod := range pods.Items {
			if pod.Spec.NodeName != "" {
				scheduledCount++
			}
		}

		t.Logf("Scheduled %d/4 pods", scheduledCount)
		return scheduledCount == 4, nil
	})

	if err != nil {
		t.Errorf("Failed to schedule all pods: %v", err)
		dumpPodStatus(t, ctx, namespace)
	}
}

// TestE2ESparkJob tests Spark job scheduling with driver and executors
func TestE2ESparkJob(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	t.Skip("Requires Spark operator - implement after integration tests pass")
}

// TestE2EHybridWorkloads tests mixed service and batch workloads
func TestE2EHybridWorkloads(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	ctx := context.Background()
	namespace := "test-hybrid-" + time.Now().Format("20060102-150405")

	// Create namespace
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	if _, err := clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{}); err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() { _ = clientset.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{}) }() //nolint:errcheck // Cleanup errors are intentionally ignored

	// Create service pods (should spread)
	t.Logf("Creating service pods...")
	// TODO: Create deployment with service workload labels

	// Create batch job (should pack)
	t.Logf("Creating batch job...")
	// TODO: Create job with batch workload labels

	// Verify service pods spread across nodes
	// Verify batch pods pack on fewer nodes

	t.Skip("Hybrid workload test not yet fully implemented")
}

// Helper functions

func createKindCluster() error {
	_, filename, _, _ := runtime.Caller(0)
	testDir := filepath.Dir(filename)
	configPath := filepath.Join(testDir, "kind-config.yaml")

	cmd := exec.Command("kind", "create", "cluster",
		"--name", "kubenexus-test",
		"--config", configPath,
		"--wait", "60s",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func cleanupKindCluster() {
	cmd := exec.Command("kind", "delete", "cluster", "--name", "kubenexus-test")
	_ = cmd.Run() //nolint:errcheck // Cleanup errors are intentionally ignored
}

func setupClient() {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = os.Getenv("HOME") + "/.kube/config"
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(err)
	}

	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}
}

func deployScheduler() error {
	// Get workspace root (two directories up from test/e2e)
	_, filename, _, _ := runtime.Caller(0)
	workspaceRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	// Build scheduler image
	cmd := exec.Command("make", "docker-build")
	cmd.Dir = workspaceRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build image: %w", err)
	}

	// Load image into Kind
	cmd = exec.Command("kind", "load", "docker-image",
		"kubenexus-scheduler:v0.1.0",
		"--name", "kubenexus-test",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to load image: %w", err)
	}

	// Apply deployment manifest
	cmd = exec.Command("kubectl", "apply", "-f", filepath.Join(workspaceRoot, "deploy", "kubenexus-scheduler.yaml"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func waitForSchedulerReady() error {
	ctx := context.Background()
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		pods, err := clientset.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{
			LabelSelector: "component=kubenexus-scheduler",
		})
		if err != nil {
			return false, err
		}

		if len(pods.Items) == 0 {
			fmt.Println("Waiting for scheduler pod to be created...")
			return false, nil
		}

		for _, pod := range pods.Items {
			if pod.Status.Phase == v1.PodRunning {
				for _, cond := range pod.Status.Conditions {
					if cond.Type == v1.PodReady && cond.Status == v1.ConditionTrue {
						fmt.Println("Scheduler is ready!")
						return true, nil
					}
				}
			}
		}

		fmt.Println("Waiting for scheduler to be ready...")
		return false, nil
	})
}

func makeGangJob(name, namespace string, parallelism int) *batchv1.Job {
	int32Parallelism := int32(parallelism)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			Parallelism: &int32Parallelism,
			Completions: &int32Parallelism,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"pod-group.scheduling.kubenexus.io/name":          name,
						"pod-group.scheduling.kubenexus.io/min-available": fmt.Sprintf("%d", parallelism),
					},
				},
				Spec: v1.PodSpec{
					SchedulerName: "kubenexus-scheduler",
					RestartPolicy: v1.RestartPolicyNever,
					Containers: []v1.Container{
						{
							Name:    "worker",
							Image:   "busybox:latest",
							Command: []string{"sh", "-c", "echo 'Training...' && sleep 30"},
						},
					},
				},
			},
		},
	}
}

func dumpPodStatus(t *testing.T, ctx context.Context, namespace string) {
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Logf("Failed to list pods: %v", err)
		return
	}

	t.Logf("Pod status dump for namespace %s:", namespace)
	for _, pod := range pods.Items {
		t.Logf("Pod %s: Phase=%s, NodeName=%s, Message=%s",
			pod.Name, pod.Status.Phase, pod.Spec.NodeName, pod.Status.Message)

		for _, cond := range pod.Status.Conditions {
			if cond.Status != v1.ConditionTrue {
				t.Logf("  Condition %s: %s - %s", cond.Type, cond.Status, cond.Message)
			}
		}
	}
}
