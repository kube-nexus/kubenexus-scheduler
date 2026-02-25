/*
Copyright 2026 KubeNexus Authors.

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

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/kube-nexus/kubenexus-scheduler/pkg/webhook"
)

var (
	port     int
	certFile string
	keyFile  string
)

func init() {
	flag.IntVar(&port, "port", 8443, "Webhook server port")
	flag.StringVar(&certFile, "tls-cert-file", "/etc/webhook/certs/tls.crt", "TLS certificate file path")
	flag.StringVar(&keyFile, "tls-key-file", "/etc/webhook/certs/tls.key", "TLS private key file path")
	klog.InitFlags(nil)
}

func main() {
	flag.Parse()

	klog.InfoS("Starting KubeNexus Admission Webhook",
		"port", port,
		"certFile", certFile,
		"keyFile", keyFile)

	config, err := rest.InClusterConfig()
	if err != nil {
		klog.ErrorS(err, "Failed to create in-cluster config")
		os.Exit(1)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.ErrorS(err, "Failed to create Kubernetes clientset")
		os.Exit(1)
	}

	podMutator := webhook.NewPodMutator(clientset)

	mux := http.NewServeMux()
	mux.HandleFunc("/mutate-pod", podMutator.Handle)
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/readyz", readyzHandler)

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		klog.ErrorS(err, "Failed to load TLS certificates")
		os.Exit(1)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		},
	}

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		TLSConfig:         tlsConfig,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second, // Prevent Slowloris attacks
	}

	go func() {
		klog.InfoS("Webhook server started", "port", port)
		if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			klog.ErrorS(err, "Failed to start webhook server")
			os.Exit(1)
		}
	}()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	<-signalChan

	klog.InfoS("Received termination signal, shutting down webhook server")

	if err := server.Close(); err != nil {
		klog.ErrorS(err, "Error shutting down webhook server")
	}
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func readyzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}
