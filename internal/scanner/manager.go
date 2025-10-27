package scanner

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	portpkg "github.com/projectdiscovery/naabu/v2/pkg/port"

	"github.com/hitushen/portnotepro/internal/models"
	"github.com/hitushen/portnotepro/internal/realtime"
	"github.com/hitushen/portnotepro/internal/services/fingerprint"
	"github.com/hitushen/portnotepro/internal/store"
)

const maxPort = 65535

// Manager 负责协调后台端口扫描任务。
type Manager struct {
	store        *store.Store
	timeout      time.Duration
	concurrency  int
	jobs         chan scanJob
	wg           sync.WaitGroup
	realtime     *realtime.Broker
	shutdownOnce sync.Once
	stopCh       chan struct{}
}

type scanJob struct {
	HostID    int64
	Ports     []models.Port
	fullRange bool
}

// NewManager 按照指定参数启动工作协程执行扫描。
func NewManager(st *store.Store, timeout time.Duration, concurrency int, broker *realtime.Broker) *Manager {
	if concurrency <= 0 {
		concurrency = 1
	}
	m := &Manager{
		store:       st,
		timeout:     timeout,
		concurrency: concurrency,
		jobs:        make(chan scanJob, concurrency*2),
		realtime:    broker,
		stopCh:      make(chan struct{}),
	}
	for i := 0; i < concurrency; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	return m
}

// ScheduleHost 为指定主机排入一次全端口扫描任务。
func (m *Manager) ScheduleHost(_ context.Context, hostID int64, _ bool) bool {
	return m.ScheduleFullRange(hostID)
}

// SchedulePorts 将一组端口加入扫描队列。
func (m *Manager) SchedulePorts(hostID int64, ports []models.Port) {
	if len(ports) == 0 {
		return
	}
	select {
	case m.jobs <- scanJob{HostID: hostID, Ports: ports}:
	case <-m.stopCh:
	}
}

// ScheduleFullRange 为主机排入 1-65535 全范围扫描。
func (m *Manager) ScheduleFullRange(hostID int64) bool {
	ok, err := m.store.BeginScan(context.Background(), hostID)
	if err != nil {
		log.Printf("[scanner] begin scan error host=%d err=%v", hostID, err)
		return false
	}
	if !ok {
		return false
	}
	m.publishScanStarted(hostID)
	select {
	case m.jobs <- scanJob{HostID: hostID, fullRange: true}:
		log.Printf("[scanner] enqueued full scan for host=%d", hostID)
	case <-m.stopCh:
		_ = m.store.EndScan(context.Background(), hostID)
	}
	return true
}

// StartTicker 启动周期任务，定期扫描所有主机。
func (m *Manager) StartTicker(interval time.Duration) {
	if interval <= 0 {
		return
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.scanAll()
			case <-m.stopCh:
				return
			}
		}
	}()
}

func (m *Manager) scanAll() {
	ctx := context.Background()
	hosts, err := m.store.ListHosts(ctx)
	if err != nil {
		return
	}
	for _, host := range hosts {
		m.ScheduleFullRange(host.ID)
	}
}

// Close 优雅停止所有扫描协程。
func (m *Manager) Close() {
	m.shutdownOnce.Do(func() {
		close(m.stopCh)
		close(m.jobs)
	})
	m.wg.Wait()
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for job := range m.jobs {
		m.handleJob(job)
	}
}

func (m *Manager) handleJob(job scanJob) {
	ctx := context.Background()
	host, err := m.store.GetHost(ctx, job.HostID)
	if err != nil {
		if job.fullRange {
			_ = m.store.EndScan(context.Background(), job.HostID)
		}
		return
	}
	if job.fullRange {
		changed, scanErr := m.scanFullRange(ctx, host)
		if scanErr != nil {
			log.Printf("[scanner] scan failed host=%d err=%v", host.ID, scanErr)
		}
		if err := m.store.EndScan(context.Background(), host.ID); err != nil {
			log.Printf("[scanner] end scan error host=%d err=%v", host.ID, err)
		}
		m.realtime.Publish(realtime.Event{
			Type:   "host_scanned",
			HostID: host.ID,
			Payload: map[string]interface{}{
				"changed":   changed,
				"success":   scanErr == nil,
				"completed": time.Now().UTC(),
			},
		})
		return
	}
	m.scanSpecificPorts(ctx, host, job.Ports)
}

func (m *Manager) scanFullRange(ctx context.Context, host *models.Host) (bool, error) {
	start := time.Now()
	log.Printf("[scanner] starting full scan host=%d addr=%s", host.ID, host.Address)
	existingPorts, err := m.store.ListPorts(ctx, host.ID, true)
	if err != nil {
		return false, err
	}

	existing := make(map[int]models.Port, len(existingPorts))
	for _, p := range existingPorts {
		existing[p.Number] = p
	}

	deadline := m.timeout * 500
	if deadline < 2*time.Minute {
		deadline = 2 * time.Minute
	}
	scanCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	naabuPorts, err := runNaabu(scanCtx, host.Address, nil)
	if err != nil {
		return false, err
	}

	checkedAt := time.Now().UTC()
	changed := false
	openSet := make(map[int]struct{}, len(naabuPorts))

	for portNum, portInfo := range naabuPorts {
		openSet[portNum] = struct{}{}
		serviceName := serviceLabel(portInfo)
		if serviceName == "" {
			serviceName = fingerprint.NameForPort(portNum)
		}
		if existingPort, ok := existing[portNum]; ok {
			if existingPort.Status != models.PortStatusOpen {
				changed = true
			}
			_ = m.store.UpdatePortStatus(ctx, existingPort.ID, models.PortStatusOpen, checkedAt)
			if serviceName != "" && serviceName != existingPort.Fingerprint {
				_ = m.store.UpdatePortFingerprint(ctx, existingPort.ID, serviceName)
			}
			m.publishStatus(host.ID, existingPort.ID, models.PortStatusOpen, checkedAt)
			continue
		}

		note := serviceName
		if note == "" {
			note = fmt.Sprintf("Port %d", portNum)
		}
		id, err := m.store.CreatePort(ctx, host.ID, portNum, note, serviceName)
		if err != nil {
			log.Printf("[scanner] create port failed host=%d port=%d err=%v", host.ID, portNum, err)
			continue
		}
		_ = m.store.UpdatePortStatus(ctx, id, models.PortStatusOpen, checkedAt)
		changed = true
		m.realtime.Publish(realtime.Event{
			Type:   "port_created",
			HostID: host.ID,
			PortID: id,
			Payload: map[string]interface{}{
				"number":      portNum,
				"fingerprint": serviceName,
			},
		})
		m.publishStatus(host.ID, id, models.PortStatusOpen, checkedAt)
	}

	for _, port := range existingPorts {
		if _, ok := openSet[port.Number]; ok {
			continue
		}
		if port.Status != models.PortStatusClosed {
			changed = true
		}
		_ = m.store.UpdatePortStatus(ctx, port.ID, models.PortStatusClosed, checkedAt)
		m.publishStatus(host.ID, port.ID, models.PortStatusClosed, checkedAt)
	}

	log.Printf("[scanner] completed naabu scan host=%d duration=%s", host.ID, time.Since(start).Truncate(time.Millisecond))
	return changed, nil
}

func (m *Manager) scanSpecificPorts(ctx context.Context, host *models.Host, ports []models.Port) {
	if len(ports) == 0 {
		return
	}
	portNums := make([]int, len(ports))
	for i, p := range ports {
		portNums[i] = p.Number
	}

	scanCtx, cancel := context.WithTimeout(ctx, m.timeout*10)
	defer cancel()

	naabuPorts, err := runNaabu(scanCtx, host.Address, portNums)
	if err != nil {
		log.Printf("[scanner] partial scan failed host=%d err=%v", host.ID, err)
		return
	}

	checkedAt := time.Now().UTC()
	for _, port := range ports {
		status := models.PortStatusClosed
		if info, ok := naabuPorts[port.Number]; ok {
			status = models.PortStatusOpen
			serviceName := serviceLabel(info)
			if serviceName == "" {
				serviceName = fingerprint.NameForPort(port.Number)
			}
			if serviceName != "" && serviceName != port.Fingerprint {
				_ = m.store.UpdatePortFingerprint(ctx, port.ID, serviceName)
			}
		}
		_ = m.store.UpdatePortStatus(ctx, port.ID, status, checkedAt)
		m.publishStatus(host.ID, port.ID, status, checkedAt)
	}
}

func (m *Manager) publishStatus(hostID, portID int64, status string, ts time.Time) {
	m.realtime.Publish(realtime.Event{
		Type:   "port_status",
		HostID: hostID,
		PortID: portID,
		Payload: map[string]interface{}{
			"status":      status,
			"lastChecked": ts,
		},
	})
}

func serviceLabel(p *portpkg.Port) string {
	if p == nil || p.Service == nil {
		return ""
	}
	svc := p.Service
	if svc.Product != "" && svc.Version != "" {
		return fmt.Sprintf("%s %s", svc.Product, svc.Version)
	}
	if svc.Product != "" {
		return svc.Product
	}
	if svc.Name != "" {
		return svc.Name
	}
	if svc.ServiceFP != "" {
		return svc.ServiceFP
	}
	if svc.ExtraInfo != "" {
		return svc.ExtraInfo
	}
	return ""
}

func (m *Manager) publishScanStarted(hostID int64) {
	m.realtime.Publish(realtime.Event{
		Type:   "host_scan_started",
		HostID: hostID,
		Payload: map[string]interface{}{
			"started": time.Now().UTC(),
		},
	})
}
