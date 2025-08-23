package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func createVMHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Dummy validations
	handlerName := "/api/v1/create"
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("X-Auth-Token") != authToken {
		logger.Error("Unauthorized access to %s", handlerName)
		incErrorCounterHandler(handlerName)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		logger.Error("Failed to parse form in %s: %s", handlerName, err.Error())
		reportError(err)
		incErrorCounterHandler(handlerName)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// 2. Check if bulk creation is requested
	countStr := r.FormValue("count")
	count := 1
	if countStr != "" {
		var err error
		count, err = strconv.Atoi(countStr)
		if err != nil || count < 1 {
			errMsg := "Invalid count parameter. Must be a positive integer"
			logger.Error(errMsg)
			reportError(errors.New(errMsg))
			incErrorCounterHandler(handlerName)
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
	}

	if count > 1 {
		logger.Info("Bulk VM creation requested: count=%d", count)
		handleBulkVMCreation(w, r, count)
		return
	}

	createSingleVM(w, r)
}

func createSingleVM(w http.ResponseWriter, r *http.Request) {
	handlerName := "/api/v1/create"

	// 1. Validate user input
	baseTemplateName := r.FormValue("base_template")
	vmTemplateName := r.FormValue("vm_template")
	vmName := r.FormValue("name")
	nodeName := r.FormValue("node")

	if baseTemplateName == "" || vmTemplateName == "" {
		errMsg := "base_template and vm_template are required"
		logger.Error(errMsg)
		reportError(errors.New(errMsg))
		incErrorCounterHandler(handlerName)
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	// 2. Node selection
	var selectedNode NodeConfig
	// 2.1 Validate user chosen node
	if nodeName != "" {
		found := false
		for _, n := range config.Nodes {
			if n.Name == nodeName {
				selectedNode = n
				found = true
				break
			}
		}
		if !found {
			errMsg := "Invalid node: " + nodeName
			logger.Error(errMsg)
			reportError(errors.New(errMsg))
			incErrorCounterHandler(handlerName)
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
	} else {
		// 2.2 Choose node by algo
		selected := selectWeightedNode(config.Nodes)
		if selected == nil {
			errMsg := "No nodes available for selection"
			logger.Error(errMsg)
			reportError(errors.New(errMsg))
			incErrorCounterHandler(handlerName)
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}
		selectedNode = *selected
		nodeName = selectedNode.Name
	}

	// 3. Chosen template validation
	var baseTemplateID int
	found := false
	for _, t := range selectedNode.BaseTemplates {
		if t.Name == baseTemplateName {
			baseTemplateID = t.ID
			found = true
			break
		}
	}
	if !found {
		errMsg := fmt.Sprintf("Invalid base_template: %s for node: %s", baseTemplateName, selectedNode.Name)
		logger.Error(errMsg)
		reportError(errors.New(errMsg))
		incErrorCounterHandler(handlerName)
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	var vmTemplateConfig VmTemplate
	found = false
	for _, t := range config.VmTemplates {
		if t.Name == vmTemplateName {
			vmTemplateConfig = t
			found = true
			break
		}
	}
	if !found {
		errMsg := "Invalid vm_template: " + vmTemplateName
		logger.Error(errMsg)
		reportError(errors.New(errMsg))
		incErrorCounterHandler(handlerName)
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	// 4. Get "next-id" for VM
	vmid, err := getNextID()
	if err != nil {
		logger.Error("Failed to get next VM id: %s", err.Error())
		reportError(err)
		incErrorCounterHandler(handlerName)
		http.Error(w, "Failed to get VM id", http.StatusInternalServerError)
		return
	}

	// 5. Set VM name
	if vmName == "" {
		randomSuffix := generateRandomString(6)
		vmName = fmt.Sprintf("%s-%s-%d-%s", vmTemplateName, selectedNode.Suffix, vmid, randomSuffix)
	}

	logger.Info("Starting VM creation: node=%s, base_template=%s, vm_template=%s, vm_name=%s",
		nodeName, baseTemplateName, vmTemplateName, vmName)

	// 6. Call & validate vm cloning
	cloneTask, err := cloneVM(nodeName, baseTemplateID, vmid, vmName)
	if err != nil {
		logger.Error("Failed to clone VM: %s", err.Error())
		reportError(err)
		incErrorCounterHandler(handlerName)
		http.Error(w, "Failed to clone VM", http.StatusInternalServerError)
		return
	}
	if err = trackTask(nodeName, cloneTask); err != nil {
		logger.Error("Clone task failed: %s", err.Error())
		reportError(err)
		incErrorCounterHandler(handlerName)
		http.Error(w, "Clone task failed", http.StatusInternalServerError)
		return
	}

	// 7. Configure CPU & memory for cloned VM
	numa := r.FormValue("numa")
	phyCores := r.FormValue("phy")
	htCores := r.FormValue("ht")

	phyOnlyStr := r.FormValue("phy_only")
	htOnlyStr := r.FormValue("ht_only")

	phyOnly := phyOnlyStr == "1"
	htOnly := htOnlyStr == "1"

	if phyOnly && htOnly {
		errMsg := "Both phy_only and ht_only cannot be set at the same time"
		logger.Error(errMsg)
		reportError(errors.New(errMsg))
		incErrorCounterHandler(handlerName)
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	configTask, err := configureVM(nodeName, vmid, vmTemplateConfig.CPU, vmTemplateConfig.Memory, vmTemplateConfig.CPUModel, numa, phyCores, htCores, phyOnly, htOnly, &selectedNode)
	if err != nil {
		logger.Error("Failed to configure VM: %s", err.Error())
		reportError(err)
		incErrorCounterHandler(handlerName)
		http.Error(w, "Failed to configure VM", http.StatusInternalServerError)
		return
	}
	if err = trackTask(nodeName, configTask); err != nil {
		logger.Error("Configuration task failed: %s", err.Error())
		reportError(err)
		incErrorCounterHandler(handlerName)
		http.Error(w, "Configuration task failed", http.StatusInternalServerError)
		return
	}

	// 8. Configure disk size
	resizeTask, err := resizeDisk(nodeName, vmid, vmTemplateConfig.Disk)
	if err != nil {
		logger.Error("Failed to resize disk: %s", err.Error())
		reportError(err)
		incErrorCounterHandler(handlerName)
		http.Error(w, "Failed to resize disk", http.StatusInternalServerError)
		return
	}
	if resizeTask != "" {
		if err = trackTask(nodeName, resizeTask); err != nil {
			logger.Error("Resize disk task failed: %s", err.Error())
			reportError(err)
			incErrorCounterHandler(handlerName)
			http.Error(w, "Resize disk task failed", http.StatusInternalServerError)
			return
		}
	}

	// 9. Start VM
	startTask, err := startVM(nodeName, vmid)
	if err != nil {
		logger.Error("Failed to start VM: %s", err.Error())
		reportError(err)
		incErrorCounterHandler(handlerName)
		http.Error(w, "Failed to start VM", http.StatusInternalServerError)
		return
	}
	if err = trackTask(nodeName, startTask); err != nil {
		logger.Error("Start VM task failed: %s", err.Error())
		reportError(err)
		incErrorCounterHandler(handlerName)
		http.Error(w, "Start VM task failed", http.StatusInternalServerError)
		return
	}

	// 10. Check if reset is requested (to fix kernel panic on first run)
	resetRequested := r.FormValue("reset") == "1"
	if resetRequested {
		logger.Info("Reset requested for VM: id=%d, node=%s, name=%s", vmid, nodeName, vmName)
		// Sleep for 3 seconds before resetting to allow VM to boot
		time.Sleep(3 * time.Second)

		// Reset the VM
		resetTask, err := resetVM(nodeName, vmid)
		if err != nil {
			logger.Error("Failed to reset VM: %s", err.Error())
			reportError(err)
			incErrorCounterHandler(handlerName)
			http.Error(w, "Failed to reset VM", http.StatusInternalServerError)
			return
		}
		if err = trackTask(nodeName, resetTask); err != nil {
			logger.Error("Reset VM task failed: %s", err.Error())
			reportError(err)
			incErrorCounterHandler(handlerName)
			http.Error(w, "Reset VM task failed", http.StatusInternalServerError)
			return
		}
		logger.Info("VM reset successful: id=%d, node=%s, name=%s", vmid, nodeName, vmName)
	}

	// 11. Get VM IP address for Talos registration
	logger.Info("Getting VM IP address for Talos registration...")
	vmIP, err := getVMIPAddressByMAC(nodeName, vmid)
	if err != nil {
		logger.Error("Failed to get VM IP address: %s", err.Error())
		reportError(err)
		incErrorCounterHandler(handlerName)
		http.Error(w, "Failed to get VM IP address", http.StatusInternalServerError)
		return
	}
	logger.Info("VM IP address obtained: %s", vmIP)

	// 12. Generate Talos configuration
	logger.Info("Generating Talos configuration...")
	talosConfig, err := generateTalosConfig(talosMachineTemplate, vmName, vmIP, vmTemplateConfig.Role)
	if err != nil {
		logger.Error("Failed to generate Talos config: %s", err.Error())
		reportError(err)
		incErrorCounterHandler(handlerName)
		http.Error(w, "Failed to generate Talos config", http.StatusInternalServerError)
		return
	}

	// 13. Wait for Talos node to be ready
	logger.Info("Waiting for Talos node to be ready...")
	if err := waitForTalosNode(vmIP); err != nil {
		logger.Error("Talos node not ready: %s", err.Error())
		reportError(err)
		incErrorCounterHandler(handlerName)
		http.Error(w, "Talos node not ready", http.StatusInternalServerError)
		return
	}

	// 14. Register node with Talos cluster
	logger.Info("Registering node with Talos cluster...")
	if err := registerTalosNode(vmIP, talosConfig, talosControlPlaneEndpoint); err != nil {
		logger.Error("Failed to register Talos node: %s", err.Error())
		reportError(err)
		incErrorCounterHandler(handlerName)
		http.Error(w, "Failed to register Talos node", http.StatusInternalServerError)
		return
	}

	// 15. Log success
	logger.Info("Talos VM creation and registration successful: id=%d, node=%s, name=%s, ip=%s, role=%s", 
		vmid, nodeName, vmName, vmIP, vmTemplateConfig.Role)
	createdCounter.With(prometheus.Labels{
		"node":          nodeName,
		"base_template": baseTemplateName,
		"vm_template":   vmTemplateName,
	}).Inc()
	respData := map[string]interface{}{
		"vm_id": vmid,
		"node":  nodeName,
		"name":  vmName,
		"ip":    vmIP,
		"role":  vmTemplateConfig.Role,
		"reset": resetRequested,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(respData)
}

type VMResult struct {
	ID    int    `json:"vm_id"`
	Node  string `json:"node"`
	Name  string `json:"name"`
	IP    string `json:"ip,omitempty"`
	Role  string `json:"role,omitempty"`
	Reset bool   `json:"reset"`
	Error string `json:"error,omitempty"`
}

func handleBulkVMCreation(w http.ResponseWriter, r *http.Request, count int) {
	handlerName := "/api/v1/create"

	baseTemplateName := r.FormValue("base_template")
	vmTemplateName := r.FormValue("vm_template")
	nodeName := r.FormValue("node")

	if baseTemplateName == "" || vmTemplateName == "" {
		errMsg := "base_template and vm_template are required"
		logger.Error(errMsg)
		reportError(errors.New(errMsg))
		incErrorCounterHandler(handlerName)
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	var selectedNode NodeConfig
	if nodeName != "" {
		found := false
		for _, n := range config.Nodes {
			if n.Name == nodeName {
				selectedNode = n
				found = true
				break
			}
		}
		if !found {
			errMsg := "Invalid node: " + nodeName
			logger.Error(errMsg)
			reportError(errors.New(errMsg))
			incErrorCounterHandler(handlerName)
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
	} else {
		selected := selectWeightedNode(config.Nodes)
		if selected == nil {
			errMsg := "No nodes available for selection"
			logger.Error(errMsg)
			reportError(errors.New(errMsg))
			incErrorCounterHandler(handlerName)
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}
		selectedNode = *selected
		nodeName = selectedNode.Name
	}

	var baseTemplateID int
	found := false
	for _, t := range selectedNode.BaseTemplates {
		if t.Name == baseTemplateName {
			baseTemplateID = t.ID
			found = true
			break
		}
	}
	if !found {
		errMsg := fmt.Sprintf("Invalid base_template: %s for node: %s", baseTemplateName, selectedNode.Name)
		logger.Error(errMsg)
		reportError(errors.New(errMsg))
		incErrorCounterHandler(handlerName)
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	var vmTemplateConfig VmTemplate
	found = false
	for _, t := range config.VmTemplates {
		if t.Name == vmTemplateName {
			vmTemplateConfig = t
			found = true
			break
		}
	}
	if !found {
		errMsg := "Invalid vm_template: " + vmTemplateName
		logger.Error(errMsg)
		reportError(errors.New(errMsg))
		incErrorCounterHandler(handlerName)
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	numa := r.FormValue("numa")
	phyCores := r.FormValue("phy")
	htCores := r.FormValue("ht")
	phyOnlyStr := r.FormValue("phy_only")
	htOnlyStr := r.FormValue("ht_only")
	phyOnly := phyOnlyStr == "1"
	htOnly := htOnlyStr == "1"

	if phyOnly && htOnly {
		errMsg := "Both phy_only and ht_only cannot be set at the same time"
		logger.Error(errMsg)
		reportError(errors.New(errMsg))
		incErrorCounterHandler(handlerName)
		http.Error(w, errMsg, http.StatusBadRequest)
		return
	}

	resetRequested := r.FormValue("reset") == "1"

	var results []VMResult

	logger.Info("Starting bulk creation of %d VMs: node=%s, base_template=%s, vm_template=%s",
		count, nodeName, baseTemplateName, vmTemplateName)

	for i := 0; i < count; i++ {
		result := VMResult{
			Node:  nodeName,
			Reset: resetRequested,
		}

		// 1. Get next VM ID
		vmid, err := getNextID()
		if err != nil {
			logger.Error("[VM %d] Failed to get next VM id: %s", i+1, err.Error())
			result.Error = "Failed to get VM id: " + err.Error()
			results = append(results, result)
			continue
		}
		result.ID = vmid

		// 2. Generate VM name
		randomSuffix := generateRandomString(6)
		vmName := fmt.Sprintf("%s-%s-%d-%s", vmTemplateName, selectedNode.Suffix, vmid, randomSuffix)
		result.Name = vmName

		logger.Info("[VM %d] Starting VM creation: node=%s, base_template=%s, vm_template=%s, vm_name=%s",
			i+1, nodeName, baseTemplateName, vmTemplateName, vmName)

		// 3. Clone VM
		cloneTask, err := cloneVM(nodeName, baseTemplateID, vmid, vmName)
		if err != nil {
			logger.Error("[VM %d] Failed to clone VM: %s", i+1, err.Error())
			result.Error = "Failed to clone VM: " + err.Error()
			results = append(results, result)
			continue
		}
		if err = trackTask(nodeName, cloneTask); err != nil {
			logger.Error("[VM %d] Clone task failed: %s", i+1, err.Error())
			result.Error = "Clone task failed: " + err.Error()
			results = append(results, result)
			continue
		}

		// 4. Configure VM
		configTask, err := configureVM(nodeName, vmid, vmTemplateConfig.CPU, vmTemplateConfig.Memory, vmTemplateConfig.CPUModel, numa, phyCores, htCores, phyOnly, htOnly, &selectedNode)
		if err != nil {
			logger.Error("[VM %d] Failed to configure VM: %s", i+1, err.Error())
			result.Error = "Failed to configure VM: " + err.Error()
			results = append(results, result)
			continue
		}
		if err = trackTask(nodeName, configTask); err != nil {
			logger.Error("[VM %d] Configuration task failed: %s", i+1, err.Error())
			result.Error = "Configuration task failed: " + err.Error()
			results = append(results, result)
			continue
		}

		// 5. Resize disk
		resizeTask, err := resizeDisk(nodeName, vmid, vmTemplateConfig.Disk)
		if err != nil {
			logger.Error("[VM %d] Failed to resize disk: %s", i+1, err.Error())
			result.Error = "Failed to resize disk: " + err.Error()
			results = append(results, result)
			continue
		}
		if resizeTask != "" {
			if err = trackTask(nodeName, resizeTask); err != nil {
				logger.Error("[VM %d] Resize disk task failed: %s", i+1, err.Error())
				result.Error = "Resize disk task failed: " + err.Error()
				results = append(results, result)
				continue
			}
		}

		// 6. Start VM
		startTask, err := startVM(nodeName, vmid)
		if err != nil {
			logger.Error("[VM %d] Failed to start VM: %s", i+1, err.Error())
			result.Error = "Failed to start VM: " + err.Error()
			results = append(results, result)
			continue
		}
		if err = trackTask(nodeName, startTask); err != nil {
			logger.Error("[VM %d] Start VM task failed: %s", i+1, err.Error())
			result.Error = "Start VM task failed: " + err.Error()
			results = append(results, result)
			continue
		}

		// 7. Reset VM if requested
		if resetRequested {
			logger.Info("[VM %d] Reset requested for VM: id=%d, node=%s, name=%s",
				i+1, vmid, nodeName, vmName)
			// Sleep for 3 seconds before resetting to allow VM to boot
			time.Sleep(3 * time.Second)

			// Reset the VM
			resetTask, err := resetVM(nodeName, vmid)
			if err != nil {
				logger.Error("[VM %d] Failed to reset VM: %s", i+1, err.Error())
				result.Error = "Reset VM failed: " + err.Error()
				results = append(results, result)
				continue
			}
			if err = trackTask(nodeName, resetTask); err != nil {
				logger.Error("[VM %d] Reset VM task failed: %s", i+1, err.Error())
				result.Error = "Reset VM task failed: " + err.Error()
				results = append(results, result)
				continue
			}
			logger.Info("[VM %d] VM reset successful: id=%d, node=%s, name=%s",
				i+1, vmid, nodeName, vmName)
		}

		// 8. VM created successfully
		logger.Info("[VM %d] VM creation successful: id=%d, node=%s, name=%s",
			i+1, vmid, nodeName, vmName)
		createdCounter.With(prometheus.Labels{
			"node":          nodeName,
			"base_template": baseTemplateName,
			"vm_template":   vmTemplateName,
		}).Inc()

		results = append(results, result)
	}

	respData := map[string]interface{}{
		"count": count,
		"vms":   results,
	}

	logger.Info("Bulk VM creation completed: created %d VMs", count)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(respData)
}

func deleteVMHandler(w http.ResponseWriter, r *http.Request) {
	// 1. Dummy validation
	handlerName := "/api/v1/delete"
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("X-Auth-Token") != authToken {
		logger.Error("Unauthorized access to %s", handlerName)
		incErrorCounterHandler(handlerName)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		logger.Error("Failed to parse form in %s: %s", handlerName, err.Error())
		reportError(err)
		incErrorCounterHandler(handlerName)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// 2. Validate user input / search VM
	vmName := r.FormValue("vm_name")
	var targetNodeName string
	var vmid int
	// 2.1 vm_name is set and that's all
	if vmName != "" {
		found := false
		for _, n := range config.Nodes {
			id, err := findVMByName(n.Name, vmName)
			if err == nil {
				targetNodeName = n.Name
				vmid = id
				found = true
				break
			}
		}
		if !found {
			errMsg := fmt.Sprintf("VM with name %s not found on any node", vmName)
			logger.Error(errMsg)
			reportError(errors.New(errMsg))
			incErrorCounterHandler(handlerName)
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
	} else {
		// 2.2 Old behaviour when vm_id and node instead of vm_name is set
		nodeName := r.FormValue("node")
		vmidStr := r.FormValue("vm_id")
		if nodeName == "" || vmidStr == "" {
			errMsg := "vm_name or (node and vm_id) are required"
			logger.Error(errMsg)
			reportError(errors.New(errMsg))
			incErrorCounterHandler(handlerName)
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
		targetNodeName = nodeName
		id, err := strconv.Atoi(vmidStr)
		if err != nil {
			errMsg := "vm_id must be a number"
			logger.Error(errMsg)
			reportError(errors.New(errMsg))
			incErrorCounterHandler(handlerName)
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
		vmid = id
	}

	// 3. Choose stop method
	stopMethod := r.FormValue("stop_method")
	if stopMethod == "" {
		stopMethod = "shutdown"
	}

	// 4. Stop VM
	logger.Info("Starting VM deletion: node=%s, vm_id=%d, stop_method=%s", targetNodeName, vmid, stopMethod)
	stopTask, err := stopVM(targetNodeName, vmid, stopMethod)
	if err != nil {
		logger.Error("Failed to stop VM: %s", err.Error())
		reportError(err)
		incErrorCounterHandler(handlerName)
		http.Error(w, "Failed to stop VM", http.StatusInternalServerError)
		return
	}
	if stopTask != "" {
		if err = trackTask(targetNodeName, stopTask); err != nil {
			logger.Error("Stop VM task failed: %s", err.Error())
			reportError(err)
			incErrorCounterHandler(handlerName)
			http.Error(w, "Stop VM task failed", http.StatusInternalServerError)
			return
		}
	} else {
		logger.Info("VM stop completed synchronously")
	}

	// 5. Delete VM
	deleteTask, err := deleteVM(targetNodeName, vmid)
	if err != nil {
		logger.Error("Failed to delete VM: %s", err.Error())
		reportError(err)
		incErrorCounterHandler(handlerName)
		http.Error(w, "Failed to delete VM", http.StatusInternalServerError)
		return
	}
	if deleteTask != "" {
		if err = trackTask(targetNodeName, deleteTask); err != nil {
			logger.Error("Delete VM task failed: %s", err.Error())
			reportError(err)
			incErrorCounterHandler(handlerName)
			http.Error(w, "Delete VM task failed", http.StatusInternalServerError)
			return
		}
	}
	deletedCounter.With(prometheus.Labels{
		"node": targetNodeName,
	}).Inc()
	logger.Info("VM deletion successful: node=%s, vm_id=%d", targetNodeName, vmid)
	respData := map[string]interface{}{
		"node":  targetNodeName,
		"vm_id": vmid,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(respData)
}

// The best health-check ever
func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("200 OK"))
}
