// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package services

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bytefreezer/goodies/log"
)

// SystemMetrics contains system resource metrics
type SystemMetrics struct {
	// Disk metrics
	DiskTotalBytes     uint64  `json:"disk_total_bytes"`
	DiskUsedBytes      uint64  `json:"disk_used_bytes"`
	DiskAvailableBytes uint64  `json:"disk_available_bytes"`
	DiskUsedPercent    float64 `json:"disk_used_percent"`

	// Memory metrics
	MemTotalBytes     uint64  `json:"mem_total_bytes"`
	MemUsedBytes      uint64  `json:"mem_used_bytes"`
	MemAvailableBytes uint64  `json:"mem_available_bytes"`
	MemUsedPercent    float64 `json:"mem_used_percent"`

	// CPU metrics
	CPUUsedPercent   float64 `json:"cpu_used_percent"`
	CPUIOWaitPercent float64 `json:"cpu_iowait_percent"`
	CPUCores         int     `json:"cpu_cores"`

	// Load average (Linux only)
	LoadAvg1  float64 `json:"load_avg_1"`
	LoadAvg5  float64 `json:"load_avg_5"`
	LoadAvg15 float64 `json:"load_avg_15"`
}

// cpuTimes stores CPU time readings for calculating usage
type cpuTimes struct {
	user   uint64
	nice   uint64
	system uint64
	idle   uint64
	iowait uint64
}

var (
	lastCPUTimes     cpuTimes
	lastCPUCheckTime time.Time
)

// CollectSystemMetrics gathers system resource metrics
func CollectSystemMetrics(diskPath string) *SystemMetrics {
	metrics := &SystemMetrics{
		CPUCores: runtime.NumCPU(),
	}

	// Collect disk metrics
	collectDiskMetrics(metrics, diskPath)

	// Collect memory metrics
	collectMemoryMetrics(metrics)

	// Collect CPU metrics
	collectCPUMetrics(metrics)

	// Collect load average
	collectLoadAverage(metrics)

	return metrics
}

// collectDiskMetrics gets disk usage for the specified path
func collectDiskMetrics(metrics *SystemMetrics, path string) {
	if path == "" {
		path = "/"
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		log.Debugf("Failed to get disk stats for %s: %v", path, err)
		return
	}

	// Bsize is int64, ensure it's positive before converting to uint64
	if stat.Bsize <= 0 {
		log.Debugf("Invalid block size for %s: %d", path, stat.Bsize)
		return
	}
	blockSize := uint64(stat.Bsize) // #nosec G115 - validated positive above

	metrics.DiskTotalBytes = stat.Blocks * blockSize
	metrics.DiskAvailableBytes = stat.Bavail * blockSize
	metrics.DiskUsedBytes = metrics.DiskTotalBytes - (stat.Bfree * blockSize)

	if metrics.DiskTotalBytes > 0 {
		metrics.DiskUsedPercent = float64(metrics.DiskUsedBytes) / float64(metrics.DiskTotalBytes) * 100
	}
}

// collectMemoryMetrics gets memory usage from /proc/meminfo
func collectMemoryMetrics(metrics *SystemMetrics) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		log.Debugf("Failed to open /proc/meminfo: %v", err)
		// Fallback to Go runtime memory stats
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		metrics.MemUsedBytes = m.Alloc
		metrics.MemTotalBytes = m.Sys
		metrics.MemAvailableBytes = m.Sys - m.Alloc
		if metrics.MemTotalBytes > 0 {
			metrics.MemUsedPercent = float64(metrics.MemUsedBytes) / float64(metrics.MemTotalBytes) * 100
		}
		return
	}
	defer file.Close()

	var memTotal, memFree, memAvailable, buffers, cached uint64

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		// Values in /proc/meminfo are in kB
		value *= 1024

		switch fields[0] {
		case "MemTotal:":
			memTotal = value
		case "MemFree:":
			memFree = value
		case "MemAvailable:":
			memAvailable = value
		case "Buffers:":
			buffers = value
		case "Cached:":
			cached = value
		}
	}

	metrics.MemTotalBytes = memTotal

	// Use MemAvailable if available (kernel 3.14+), otherwise calculate
	if memAvailable > 0 {
		metrics.MemAvailableBytes = memAvailable
	} else {
		metrics.MemAvailableBytes = memFree + buffers + cached
	}

	metrics.MemUsedBytes = memTotal - metrics.MemAvailableBytes

	if memTotal > 0 {
		metrics.MemUsedPercent = float64(metrics.MemUsedBytes) / float64(memTotal) * 100
	}
}

// collectCPUMetrics calculates CPU usage from /proc/stat
func collectCPUMetrics(metrics *SystemMetrics) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		log.Debugf("Failed to open /proc/stat: %v", err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 5 {
			return
		}

		current := cpuTimes{}
		current.user, _ = strconv.ParseUint(fields[1], 10, 64)
		current.nice, _ = strconv.ParseUint(fields[2], 10, 64)
		current.system, _ = strconv.ParseUint(fields[3], 10, 64)
		current.idle, _ = strconv.ParseUint(fields[4], 10, 64)
		if len(fields) > 5 {
			current.iowait, _ = strconv.ParseUint(fields[5], 10, 64)
		}

		// Calculate CPU usage since last check
		now := time.Now()
		if !lastCPUCheckTime.IsZero() {
			totalDelta := (current.user - lastCPUTimes.user) +
				(current.nice - lastCPUTimes.nice) +
				(current.system - lastCPUTimes.system) +
				(current.idle - lastCPUTimes.idle) +
				(current.iowait - lastCPUTimes.iowait)

			idleDelta := (current.idle - lastCPUTimes.idle) + (current.iowait - lastCPUTimes.iowait)
			iowaitDelta := current.iowait - lastCPUTimes.iowait

			if totalDelta > 0 {
				metrics.CPUUsedPercent = float64(totalDelta-idleDelta) / float64(totalDelta) * 100
				metrics.CPUIOWaitPercent = float64(iowaitDelta) / float64(totalDelta) * 100
			}
		}

		lastCPUTimes = current
		lastCPUCheckTime = now
		break
	}
}

// collectLoadAverage gets system load average from /proc/loadavg
func collectLoadAverage(metrics *SystemMetrics) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		log.Debugf("Failed to read /proc/loadavg: %v", err)
		return
	}

	fields := strings.Fields(string(data))
	if len(fields) >= 3 {
		metrics.LoadAvg1, _ = strconv.ParseFloat(fields[0], 64)
		metrics.LoadAvg5, _ = strconv.ParseFloat(fields[1], 64)
		metrics.LoadAvg15, _ = strconv.ParseFloat(fields[2], 64)
	}
}

// ToMap converts SystemMetrics to a map for inclusion in health reports
func (m *SystemMetrics) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"disk_total_bytes":     m.DiskTotalBytes,
		"disk_used_bytes":      m.DiskUsedBytes,
		"disk_available_bytes": m.DiskAvailableBytes,
		"disk_used_percent":    m.DiskUsedPercent,
		"mem_total_bytes":      m.MemTotalBytes,
		"mem_used_bytes":       m.MemUsedBytes,
		"mem_available_bytes":  m.MemAvailableBytes,
		"mem_used_percent":     m.MemUsedPercent,
		"cpu_used_percent":     m.CPUUsedPercent,
		"cpu_iowait_percent":   m.CPUIOWaitPercent,
		"cpu_cores":            m.CPUCores,
		"load_avg_1":           m.LoadAvg1,
		"load_avg_5":           m.LoadAvg5,
		"load_avg_15":          m.LoadAvg15,
	}
}

// UDPSocketStats contains UDP socket statistics for a port
type UDPSocketStats struct {
	Port       int   `json:"port"`
	Drops      int64 `json:"drops"`
	RxQueue    int64 `json:"rx_queue"`
	RxBufferKB int64 `json:"rx_buffer_kb"`
}

// CollectUDPSocketDrops reads /proc/net/udp and /proc/net/udp6 and returns drop counts for specified ports
func CollectUDPSocketDrops(ports []int) map[int]*UDPSocketStats {
	result := make(map[int]*UDPSocketStats)

	// Initialize result for all requested ports
	for _, port := range ports {
		result[port] = &UDPSocketStats{Port: port}
	}

	// Create a set of ports for quick lookup (in hex)
	portSet := make(map[string]int)
	for _, port := range ports {
		// Convert port to hex format used in /proc/net/udp (uppercase, 4 chars)
		hexPort := strings.ToUpper(strconv.FormatInt(int64(port), 16))
		// Pad to 4 characters
		for len(hexPort) < 4 {
			hexPort = "0" + hexPort
		}
		portSet[hexPort] = port
	}

	// Read both IPv4 and IPv6 UDP sockets
	// Sockets bound to *:port appear in udp6, not udp
	parseUDPFile("/proc/net/udp", portSet, result)
	parseUDPFile("/proc/net/udp6", portSet, result)

	return result
}

// parseUDPFile reads a /proc/net/udp or /proc/net/udp6 file and updates stats
func parseUDPFile(filePath string, portSet map[string]int, result map[int]*UDPSocketStats) {
	file, err := os.Open(filePath) // #nosec G304 - filePath is always a hardcoded /proc path
	if err != nil {
		log.Debugf("Failed to open %s: %v", filePath, err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum == 1 {
			// Skip header line
			continue
		}

		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 13 {
			continue
		}

		// local_address is field[1] in format "IP:PORT" (hex)
		// IPv4: 00000000:0808
		// IPv6: 00000000000000000000000000000000:0808
		localAddr := fields[1]
		parts := strings.Split(localAddr, ":")
		if len(parts) != 2 {
			continue
		}

		hexPort := strings.ToUpper(parts[1])
		port, exists := portSet[hexPort]
		if !exists {
			continue
		}

		// rx_queue is field[4] in format "tx_queue:rx_queue" (hex)
		queues := strings.Split(fields[4], ":")
		if len(queues) == 2 {
			rxQueue, _ := strconv.ParseInt(queues[1], 16, 64)
			// Add to existing value (could have entries in both files)
			result[port].RxQueue += rxQueue
		}

		// drops is the last field (field[12] or later depending on kernel)
		// Format: sl local_address rem_address st tx_queue:rx_queue tr tm->when retrnsmt uid timeout inode ref pointer drops
		dropsField := fields[len(fields)-1]
		drops, _ := strconv.ParseInt(dropsField, 10, 64)
		// Add to existing value (could have entries in both files)
		result[port].Drops += drops
	}
}

// GetTotalUDPDrops returns total drops across all specified ports
func GetTotalUDPDrops(ports []int) int64 {
	stats := CollectUDPSocketDrops(ports)
	var total int64
	for _, s := range stats {
		total += s.Drops
	}
	return total
}
