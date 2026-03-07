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

// Package vramscheduler provides DRA (Dynamic Resource Allocation) integration
// for GPU topology discovery and VRAM-aware scheduling.
package vramscheduler

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	klog "k8s.io/klog/v2"
)

// getGPUTopologyFromDRA extracts complete GPU topology from DRA ResourceSlices.
// This is the PRIMARY method for topology discovery in Kubernetes 1.26+.
//
// Returns:
//   - vramPerGPU: Per-GPU VRAM capacity in bytes
//   - devices: Slice of GPUDevice with full topology information
//   - error: Non-nil if ResourceSlices couldn't be queried
func (v *VRAMScheduler) getGPUTopologyFromDRA(ctx context.Context, node *v1.Node) (int64, []GPUDevice, error) {
	// Use pre-initialized lister from struct (set once in New())
	if v.resourceSliceLister == nil {
		return 0, nil, fmt.Errorf("ResourceSlice lister not available")
	}

	// List all ResourceSlices and filter by node name
	// Note: Field selectors are not supported by listers, so we filter after listing
	allResourceSlices, err := v.resourceSliceLister.List(labels.Everything())
	if err != nil {
		return 0, nil, fmt.Errorf("failed to list ResourceSlices: %w", err)
	}

	// Filter ResourceSlices for this specific node
	var resourceSlices []*resourcev1.ResourceSlice
	for _, slice := range allResourceSlices {
		if slice.Spec.NodeName != nil && *slice.Spec.NodeName == node.Name {
			resourceSlices = append(resourceSlices, slice)
		}
	}

	if len(resourceSlices) == 0 {
		return 0, nil, fmt.Errorf("no ResourceSlices found for node %s", node.Name)
	}

	var gpuDevices []GPUDevice
	var vramPerGPU int64

	// Iterate through ResourceSlices to find GPU resources
	for _, slice := range resourceSlices {
		// Only process GPU drivers (nvidia, amd, intel, etc.)
		if !isGPUDriver(slice.Spec.Driver) {
			klog.V(6).InfoS("Skipping non-GPU ResourceSlice",
				"node", node.Name,
				"driver", slice.Spec.Driver)
			continue
		}

		klog.V(5).InfoS("Processing GPU ResourceSlice",
			"node", node.Name,
			"driver", slice.Spec.Driver,
			"deviceCount", len(slice.Spec.Devices))

		// Extract GPU devices from this slice
		for _, device := range slice.Spec.Devices {
			gpu := v.parseGPUDeviceFromDRA(device, slice.Spec.Driver)

			if gpu.VRAM > 0 {
				gpuDevices = append(gpuDevices, gpu)

				// Track minimum VRAM for heterogeneous GPU nodes
				if vramPerGPU == 0 || gpu.VRAM < vramPerGPU {
					vramPerGPU = gpu.VRAM
				}

				klog.V(5).InfoS("Discovered GPU device from DRA",
					"node", node.Name,
					"gpu", gpu.Name,
					"vram", formatBytes(gpu.VRAM),
					"numaNode", gpu.NUMANode,
					"nvlinkDomain", gpu.NVLinkDomain,
					"nvlinkPeers", len(gpu.NVLinkPeers),
					"pcieSwitch", gpu.PCIeSwitch)
			}
		}
	}

	if len(gpuDevices) == 0 {
		return 0, nil, fmt.Errorf("no GPU devices found in ResourceSlices for node %s", node.Name)
	}

	klog.V(4).InfoS("Successfully extracted GPU topology from DRA ResourceSlices",
		"node", node.Name,
		"gpuCount", len(gpuDevices),
		"vramPerGPU", formatBytes(vramPerGPU))

	return vramPerGPU, gpuDevices, nil
}

// parseGPUDeviceFromDRA extracts a GPUDevice from a DRA Device specification
func (v *VRAMScheduler) parseGPUDeviceFromDRA(device resourcev1.Device, driver string) GPUDevice {
	gpu := GPUDevice{
		Name:         device.Name,
		NUMANode:     -1,         // Default: unknown
		NVLinkDomain: -1,         // Default: unknown
		NVLinkPeers:  []string{}, // Default: no peers
	}

	// Extract VRAM capacity from device capacity
	if device.Capacity != nil {
		// Try common VRAM resource names (QualifiedName is string type alias)
		vramResourceNames := []resourcev1.QualifiedName{
			"memory",            // DRA standard
			"gpu.memory",        // Alternative
			"nvidia.com/memory", // NVIDIA-specific
			"vram",              // Generic
		}

		for _, resName := range vramResourceNames {
			if vramCapacity, exists := device.Capacity[resName]; exists {
				gpu.VRAM = vramCapacity.Value.Value()
				break
			}
		}
	}

	// Extract topology attributes
	if device.Attributes != nil {
		// NUMA node
		if numaAttr, exists := device.Attributes["numa-node"]; exists {
			if numaAttr.IntValue != nil {
				gpu.NUMANode = int(*numaAttr.IntValue)
			} else if numaAttr.StringValue != nil {
				if numa, err := strconv.Atoi(*numaAttr.StringValue); err == nil {
					gpu.NUMANode = numa
				}
			}
		}

		// PCIe bus ID
		if pcieAttr, exists := device.Attributes["pcie-bus-id"]; exists && pcieAttr.StringValue != nil {
			gpu.PCIeBusID = *pcieAttr.StringValue
		}

		// PCIe switch identifier
		if switchAttr, exists := device.Attributes["pcie-switch"]; exists && switchAttr.StringValue != nil {
			gpu.PCIeSwitch = *switchAttr.StringValue
		}

		// NVLink topology
		if peersAttr, exists := device.Attributes["nvlink-peers"]; exists && peersAttr.StringValue != nil {
			// Parse comma-separated list of peer GPU names
			peerList := strings.Split(*peersAttr.StringValue, ",")
			for _, peer := range peerList {
				peer = strings.TrimSpace(peer)
				if peer != "" {
					gpu.NVLinkPeers = append(gpu.NVLinkPeers, peer)
				}
			}
		}

		// NVLink domain/island
		if domainAttr, exists := device.Attributes["nvlink-domain"]; exists {
			if domainAttr.IntValue != nil {
				gpu.NVLinkDomain = int(*domainAttr.IntValue)
			} else if domainAttr.StringValue != nil {
				if domain, err := strconv.Atoi(*domainAttr.StringValue); err == nil {
					gpu.NVLinkDomain = domain
				}
			}
		}

		// GPU model/type (optional, for informational purposes)
		if modelAttr, exists := device.Attributes["model"]; exists && modelAttr.StringValue != nil {
			// Store in PCIeSwitch field if not already populated (reuse field)
			if gpu.PCIeSwitch == "" {
				gpu.PCIeSwitch = *modelAttr.StringValue
			}
		}
	}

	return gpu
}

// getVRAMFromResourceClaim extracts VRAM requirement from pod's ResourceClaim.
// This is the PRIMARY method for getting VRAM requirements in DRA-enabled clusters.
//
// Returns VRAM in bytes, or 0 if no ResourceClaim found or no memory requirement specified.
func (v *VRAMScheduler) getVRAMFromResourceClaim(ctx context.Context, pod *v1.Pod) (int64, error) {
	if len(pod.Spec.ResourceClaims) == 0 {
		return 0, fmt.Errorf("no ResourceClaims in pod spec")
	}

	// Use pre-initialized listers from struct (set once in New())
	if v.resourceClaimLister == nil || v.resourceClaimTemplateLister == nil {
		return 0, fmt.Errorf("ResourceClaim listers not available")
	}

	for _, claimRef := range pod.Spec.ResourceClaims {
		klog.V(5).InfoS("Processing ResourceClaim",
			"pod", klog.KObj(pod),
			"claim", claimRef.Name)

		// Strategy 1: If claim already exists, try to parse it (use lister)
		claim, err := v.resourceClaimLister.ResourceClaims(pod.Namespace).Get(claimRef.Name)
		if err == nil {
			// Claim exists - try to extract VRAM from allocation
			vram := v.extractVRAMFromClaimAllocation(claim)
			if vram > 0 {
				klog.V(4).InfoS("Extracted VRAM from ResourceClaim allocation",
					"pod", klog.KObj(pod),
					"claim", claimRef.Name,
					"vram", formatBytes(vram))
				return vram, nil
			}
		}

		// Strategy 2: Parse ResourceClaimTemplate (inline or referenced)
		var templateSpec *resourcev1.DeviceClaim

		if claimRef.ResourceClaimTemplateName != nil {
			// External template reference (use lister)
			template, err := v.resourceClaimTemplateLister.ResourceClaimTemplates(pod.Namespace).Get(
				*claimRef.ResourceClaimTemplateName)
			if err != nil {
				klog.V(5).InfoS("Could not fetch ResourceClaimTemplate",
					"pod", klog.KObj(pod),
					"template", *claimRef.ResourceClaimTemplateName,
					"error", err)
				continue
			}
			// template.Spec is ResourceClaimTemplateSpec which has Spec (ResourceClaimSpec)
			// ResourceClaimSpec has Devices (DeviceClaim)
			templateSpec = &template.Spec.Spec.Devices
		}

		// Try to extract VRAM from template spec
		if templateSpec != nil {
			vram := v.extractVRAMFromDeviceClaim(templateSpec)
			if vram > 0 {
				klog.V(4).InfoS("Extracted VRAM from ResourceClaimTemplate",
					"pod", klog.KObj(pod),
					"claim", claimRef.Name,
					"vram", formatBytes(vram))
				return vram, nil
			}
		}

		// Strategy 3: Heuristic - if this is a GPU claim, check annotation hint
		if v.isGPUClaim(claimRef.Name, templateSpec) {
			if vramStr, ok := pod.Annotations[AnnotationVRAMRequest]; ok {
				quantity, err := resource.ParseQuantity(vramStr)
				if err == nil {
					klog.V(4).InfoS("Using VRAM annotation hint for GPU ResourceClaim",
						"pod", klog.KObj(pod),
						"claim", claimRef.Name,
						"vram", formatBytes(quantity.Value()))
					return quantity.Value(), nil
				}
			}
		}
	}

	return 0, fmt.Errorf("no VRAM requirement found in ResourceClaims")
}

// extractVRAMFromClaimAllocation extracts VRAM from an allocated ResourceClaim
func (v *VRAMScheduler) extractVRAMFromClaimAllocation(claim *resourcev1.ResourceClaim) int64 {
	if claim.Status.Allocation == nil {
		return 0
	}

	// Parse allocated devices
	for _, result := range claim.Status.Allocation.Devices.Results {
		// Check if this is a GPU device
		if !strings.Contains(strings.ToLower(result.Driver), "gpu") &&
			!strings.Contains(strings.ToLower(result.Driver), "nvidia") &&
			!strings.Contains(strings.ToLower(result.Driver), "accelerator") {
			continue
		}

		// Look for memory/VRAM in device attributes or config
		if result.Device != "" {
			// Device allocated - we'd need to query the device's capacity
			// This requires fetching the ResourceSlice, which we do in getGPUTopologyFromDRA
			// For now, return 0 and rely on topology discovery
			klog.V(6).InfoS("Found allocated GPU device, but VRAM should come from ResourceSlice topology",
				"device", result.Device,
				"driver", result.Driver)
		}
	}

	return 0
}

// extractVRAMFromDeviceClaim extracts VRAM requirement from DeviceClaim spec
func (v *VRAMScheduler) extractVRAMFromDeviceClaim(deviceClaim *resourcev1.DeviceClaim) int64 {
	if deviceClaim == nil {
		return 0
	}

	// Iterate through device requests
	for _, request := range deviceClaim.Requests {
		// Handle Exactly mode (single request)
		if request.Exactly != nil {
			// Check selectors for memory constraints in Exactly request
			for _, selector := range request.Exactly.Selectors {
				// Parse CEL expression for device capacity constraints
				if selector.CEL != nil && selector.CEL.Expression != "" {
					vram := v.parseVRAMFromCEL(selector.CEL.Expression)
					if vram > 0 {
						return vram
					}
				}
			}
		}

		// Handle FirstAvailable mode (fallback options)
		for _, subRequest := range request.FirstAvailable {
			for _, selector := range subRequest.Selectors {
				if selector.CEL != nil && selector.CEL.Expression != "" {
					vram := v.parseVRAMFromCEL(selector.CEL.Expression)
					if vram > 0 {
						return vram
					}
				}
			}
		}
	}

	// Note: DeviceClaim.Constraints do not have CEL expressions.
	// They are used for matching attributes across devices (e.g., same NUMA node).
	// VRAM requirements are specified via CEL in request selectors.

	return 0
}

// parseVRAMFromCEL extracts VRAM value from CEL expression
// Handles patterns like:
//   - device.capacity["memory"] >= "80Gi"
//   - device.capacity["vram"] == "40GB"
//   - device.attributes["memory"] > "24Gi"
func (v *VRAMScheduler) parseVRAMFromCEL(expr string) int64 {
	// Common memory-related attribute names in CEL
	memoryPatterns := []string{
		`capacity["memory"]`,
		`capacity['memory']`,
		`capacity["vram"]`,
		`capacity['vram']`,
		`capacity["gpu.memory"]`,
		`attributes["memory"]`,
		`attributes['memory']`,
	}

	for _, pattern := range memoryPatterns {
		if !strings.Contains(expr, pattern) {
			continue
		}

		// Extract the quantity value after comparison operators
		for _, op := range []string{">=", "==", ">", "<=", "="} {
			parts := strings.Split(expr, op)
			if len(parts) < 2 {
				continue
			}

			// Check if pattern is in the left side
			if !strings.Contains(parts[0], pattern) {
				continue
			}

			// Extract quoted quantity from right side
			rightSide := strings.TrimSpace(parts[1])
			vram := v.extractQuantityFromString(rightSide)
			if vram > 0 {
				klog.V(5).InfoS("Parsed VRAM from CEL expression",
					"expression", expr,
					"vram", formatBytes(vram))
				return vram
			}
		}
	}

	return 0
}

// extractQuantityFromString extracts a Kubernetes quantity from a string
// Handles: "80Gi", '24GB', resource.quantity("40Gi"), etc.
func (v *VRAMScheduler) extractQuantityFromString(s string) int64 {
	s = strings.TrimSpace(s)

	// Remove function calls like resource.quantity("40Gi")
	if strings.Contains(s, "quantity(") {
		start := strings.Index(s, "quantity(\"")
		if start >= 0 {
			start += len("quantity(\"")
			end := strings.Index(s[start:], "\"")
			if end > 0 {
				s = s[start : start+end]
			}
		}
	}

	// Extract quoted string
	s = strings.Trim(s, " \t\n")
	if len(s) > 0 && (s[0] == '"' || s[0] == '\'') {
		quote := s[0]
		s = strings.Trim(s, string(quote))
	}

	// Handle trailing operators/parentheses
	s = strings.TrimRight(s, " )")

	// Parse as Kubernetes quantity
	quantity, err := resource.ParseQuantity(s)
	if err != nil {
		klog.V(6).InfoS("Failed to parse quantity",
			"string", s,
			"error", err)
		return 0
	}

	return quantity.Value()
}

// isGPUClaim checks if a ResourceClaim is for GPU resources
func (v *VRAMScheduler) isGPUClaim(claimName string, deviceClaim *resourcev1.DeviceClaim) bool {
	// Check claim name
	nameLower := strings.ToLower(claimName)
	if strings.Contains(nameLower, "gpu") ||
		strings.Contains(nameLower, "accelerator") ||
		strings.Contains(nameLower, "nvidia") {
		return true
	}

	// Check device class in requests
	if deviceClaim != nil {
		for _, request := range deviceClaim.Requests {
			var className string
			// Check Exactly mode
			if request.Exactly != nil {
				className = strings.ToLower(request.Exactly.DeviceClassName)
				if strings.Contains(className, "gpu") ||
					strings.Contains(className, "nvidia") ||
					strings.Contains(className, "accelerator") {
					return true
				}
			}
			// Check FirstAvailable mode
			for _, subRequest := range request.FirstAvailable {
				className = strings.ToLower(subRequest.DeviceClassName)
				if strings.Contains(className, "gpu") ||
					strings.Contains(className, "nvidia") ||
					strings.Contains(className, "accelerator") {
					return true
				}
			}
		}
	}

	return false
}
