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

// Package vramscheduler provides NFD (Node Feature Discovery) integration
// for GPU topology auto-discovery as a fallback when DRA is not available.
package vramscheduler

import (
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"
)

// NFD label prefixes and patterns
const (
	// NFD standard label prefix
	NFDLabelPrefix = "feature.node.kubernetes.io/"

	// PCI device labels (NFD auto-detects PCI devices)
	// Format: feature.node.kubernetes.io/pci-<vendor_id>.device.<device_id>.present = "true"
	NFDPCIPrefix = NFDLabelPrefix + "pci-"

	// NVIDIA vendor ID in PCI
	NVIDIAVendorID = "10de"

	// AMD vendor ID in PCI
	AMDVendorID = "1002"

	// Intel GPU vendor ID in PCI
	IntelGPUVendorID = "8086"

	// CPU topology from NFD
	NFDCPUTopologyPrefix = NFDLabelPrefix + "cpu-"
	NFDNUMANodes         = NFDLabelPrefix + "memory-numa"

	// System topology
	NFDSystemPrefix = NFDLabelPrefix + "system-"
)

// Known GPU PCI device IDs to VRAM mapping
// Source: https://pci-ids.ucw.cz/ and GPU vendor specifications
var nvidiaDeviceIDToVRAM = map[string]int64{
	// H100 series
	"2330": 80 * 1024 * 1024 * 1024,  // H100 PCIe 80GB
	"2331": 80 * 1024 * 1024 * 1024,  // H100 SXM5 80GB
	"2322": 141 * 1024 * 1024 * 1024, // H200 141GB

	// A100 series
	"20b0": 40 * 1024 * 1024 * 1024, // A100 PCIe 40GB
	"20b1": 40 * 1024 * 1024 * 1024, // A100 SXM4 40GB
	"20b2": 80 * 1024 * 1024 * 1024, // A100 PCIe 80GB
	"20b5": 80 * 1024 * 1024 * 1024, // A100 SXM4 80GB

	// L40/L40S series
	"26b5": 48 * 1024 * 1024 * 1024, // L40S 48GB
	"26b1": 48 * 1024 * 1024 * 1024, // L40 48GB

	// L4 series
	"27b8": 24 * 1024 * 1024 * 1024, // L4 24GB

	// A40 series
	"2235": 48 * 1024 * 1024 * 1024, // A40 48GB

	// A30 series
	"20b7": 24 * 1024 * 1024 * 1024, // A30 24GB

	// T4 series
	"1eb8": 16 * 1024 * 1024 * 1024, // T4 16GB

	// V100 series
	"1db4": 16 * 1024 * 1024 * 1024, // V100 PCIe 16GB
	"1db5": 32 * 1024 * 1024 * 1024, // V100 SXM2 32GB
	"1db6": 32 * 1024 * 1024 * 1024, // V100 SXM2 32GB (variant)

	// RTX series (datacenter)
	"2204": 48 * 1024 * 1024 * 1024, // RTX 6000 Ada 48GB
	"2206": 48 * 1024 * 1024 * 1024, // RTX 5000 Ada 32GB (approximate)
}

// getTopologyFromNFD extracts GPU topology from NFD-populated node labels.
// This is the SECONDARY fallback when DRA is not available (K8s < 1.26 or no DRA driver).
//
// NFD auto-discovers hardware via:
//   - PCI device scanning (finds GPUs by vendor/device ID)
//   - CPU topology (NUMA nodes)
//   - System information
//
// Returns (vramPerGPU, []GPUDevice)
func (v *VRAMScheduler) getTopologyFromNFD(node *v1.Node) (int64, []GPUDevice, error) {
	// Scan for PCI GPU devices in NFD labels
	var gpuDevices []GPUDevice
	var vramPerGPU int64

	// Check for NVIDIA GPUs
	nvidiaDevices := v.scanNFDForPCIDevices(node, NVIDIAVendorID)
	gpuDevices = append(gpuDevices, nvidiaDevices...)

	// Check for AMD GPUs
	amdDevices := v.scanNFDForPCIDevices(node, AMDVendorID)
	gpuDevices = append(gpuDevices, amdDevices...)

	// Check for Intel GPUs (Arc, Flex, Max)
	intelDevices := v.scanNFDForPCIDevices(node, IntelGPUVendorID)
	gpuDevices = append(gpuDevices, intelDevices...)

	if len(gpuDevices) == 0 {
		return 0, nil, fmt.Errorf("no GPU devices found in NFD labels for node %s", node.Name)
	}

	// Calculate minimum VRAM (for heterogeneous nodes)
	for _, gpu := range gpuDevices {
		if gpu.VRAM > 0 && (vramPerGPU == 0 || gpu.VRAM < vramPerGPU) {
			vramPerGPU = gpu.VRAM
		}
	}

	// Try to extract NUMA topology from NFD
	numaNodeCount := v.getNUMANodeCountFromNFD(node)
	if numaNodeCount > 0 {
		// Distribute GPUs across NUMA nodes (best guess)
		gpusPerNUMA := len(gpuDevices) / numaNodeCount
		for i := range gpuDevices {
			if gpusPerNUMA > 0 {
				gpuDevices[i].NUMANode = i / gpusPerNUMA
			}
		}
		klog.V(5).InfoS("Inferred NUMA distribution from NFD",
			"node", node.Name,
			"numaNodes", numaNodeCount,
			"gpuCount", len(gpuDevices))
	}

	klog.V(4).InfoS("Successfully extracted GPU topology from NFD labels",
		"node", node.Name,
		"gpuCount", len(gpuDevices),
		"vramPerGPU", formatBytes(vramPerGPU))

	return vramPerGPU, gpuDevices, nil
}

// scanNFDForPCIDevices scans NFD labels for PCI devices matching a vendor ID
func (v *VRAMScheduler) scanNFDForPCIDevices(node *v1.Node, vendorID string) []GPUDevice {
	var devices []GPUDevice
	deviceIndex := 0

	// NFD creates labels like: feature.node.kubernetes.io/pci-10de.device.2330.present = "true"
	// Where 10de = NVIDIA vendor ID, 2330 = H100 device ID
	pciPrefix := fmt.Sprintf("%spci-%s.device.", NFDLabelPrefix, vendorID)

	for label, value := range node.Labels {
		if !strings.HasPrefix(label, pciPrefix) {
			continue
		}

		// Label format: feature.node.kubernetes.io/pci-<vendor>.device.<device_id>.present
		// or: feature.node.kubernetes.io/pci-<vendor>.device.<device_id>.count
		if !strings.HasSuffix(label, ".present") && !strings.HasSuffix(label, ".count") {
			continue
		}

		if value != "true" && value == "" {
			continue
		}

		// Extract device ID
		parts := strings.Split(label, ".")
		if len(parts) < 4 {
			continue
		}
		deviceID := parts[len(parts)-2] // Second to last part is device ID

		// Get device count (number of this GPU type on the node)
		deviceCount := 1
		countLabel := pciPrefix + deviceID + ".count"
		if countStr, ok := node.Labels[countLabel]; ok {
			if count, err := strconv.Atoi(countStr); err == nil && count > 0 {
				deviceCount = count
			}
		}

		// Look up VRAM for this device ID
		var vram int64
		switch vendorID {
		case NVIDIAVendorID:
			vram = nvidiaDeviceIDToVRAM[deviceID]
		case AMDVendorID:
			vram = inferAMDVRAMFromDeviceID(deviceID)
		case IntelGPUVendorID:
			vram = inferIntelVRAMFromDeviceID(deviceID)
		}

		if vram == 0 {
			klog.V(5).InfoS("Unknown GPU device ID, VRAM unknown",
				"node", node.Name,
				"vendorID", vendorID,
				"deviceID", deviceID)
			continue
		}

		// Create GPU device entries
		for i := 0; i < deviceCount; i++ {
			gpu := GPUDevice{
				Name:         fmt.Sprintf("gpu-%d", deviceIndex),
				VRAM:         vram,
				PCIeBusID:    fmt.Sprintf("%s:%s-%d", vendorID, deviceID, i),
				NUMANode:     -1, // Will be inferred later if NUMA info available
				NVLinkDomain: -1,
				NVLinkPeers:  []string{},
			}
			devices = append(devices, gpu)
			deviceIndex++
		}

		klog.V(5).InfoS("Discovered GPU from NFD PCI labels",
			"node", node.Name,
			"vendorID", vendorID,
			"deviceID", deviceID,
			"count", deviceCount,
			"vram", formatBytes(vram))
	}

	return devices
}

// getNUMANodeCountFromNFD extracts NUMA node count from NFD labels
func (v *VRAMScheduler) getNUMANodeCountFromNFD(node *v1.Node) int {
	// NFD sets: feature.node.kubernetes.io/memory-numa = "true"
	// and optionally: feature.node.kubernetes.io/memory-numa.node_count = "2"
	if _, hasNUMA := node.Labels[NFDNUMANodes]; !hasNUMA {
		return 0
	}

	// Try to get NUMA node count
	numaCountLabel := NFDNUMANodes + ".node_count"
	if countStr, ok := node.Labels[numaCountLabel]; ok {
		if count, err := strconv.Atoi(countStr); err == nil && count > 0 {
			return count
		}
	}

	// Fallback: check for individual NUMA node labels
	// feature.node.kubernetes.io/memory-numa.node0 = "true"
	// feature.node.kubernetes.io/memory-numa.node1 = "true"
	maxNode := 0
	for label := range node.Labels {
		if strings.HasPrefix(label, NFDNUMANodes+".node") {
			nodeNumStr := strings.TrimPrefix(label, NFDNUMANodes+".node")
			if nodeNum, err := strconv.Atoi(nodeNumStr); err == nil {
				if nodeNum > maxNode {
					maxNode = nodeNum
				}
			}
		}
	}

	if maxNode > 0 {
		return maxNode + 1 // Node IDs are 0-indexed
	}

	return 0
}

// inferAMDVRAMFromDeviceID infers VRAM for AMD GPUs
// TODO: Expand this mapping as needed
func inferAMDVRAMFromDeviceID(deviceID string) int64 {
	amdDeviceMap := map[string]int64{
		"740f": 32 * 1024 * 1024 * 1024, // MI300X 192GB (HBM3)
		"740c": 64 * 1024 * 1024 * 1024, // MI250X 128GB
		"7408": 32 * 1024 * 1024 * 1024, // MI250 64GB
		"738c": 32 * 1024 * 1024 * 1024, // MI100 32GB
	}

	if vram, ok := amdDeviceMap[deviceID]; ok {
		return vram
	}
	return 0
}

// inferIntelVRAMFromDeviceID infers VRAM for Intel GPUs
// TODO: Expand this mapping as needed
func inferIntelVRAMFromDeviceID(deviceID string) int64 {
	intelDeviceMap := map[string]int64{
		"0bd5": 48 * 1024 * 1024 * 1024, // Data Center GPU Max 1550 (Ponte Vecchio)
		"56c0": 16 * 1024 * 1024 * 1024, // Arc A770 16GB
		"56c1": 12 * 1024 * 1024 * 1024, // Arc A750 8GB
	}

	if vram, ok := intelDeviceMap[deviceID]; ok {
		return vram
	}
	return 0
}
