package realtime

import (
	"encoding/json"
	"sync"
)

// Event 描述 SSE 推送时的消息载荷。
type Event struct {
	Type    string      `json:"type"`
	HostID  int64       `json:"hostId,omitempty"`
	PortID  int64       `json:"portId,omitempty"`
	Payload interface{} `json:"payload,omitempty"`
}

// Broker 负责向实时订阅者（SSE 客户端）分发事件。
type Broker struct {
	mu       sync.RWMutex
	clients  map[chan []byte]struct{}
	shutdown chan struct{}
}

// NewBroker 创建一个新的 Broker 实例。
func NewBroker() *Broker {
	return newBroker(make(map[chan []byte]struct{}))
}

func newBroker(clients map[chan []byte]struct{}) *Broker {
	return &Broker{
		clients:  clients,
		shutdown: make(chan struct{}),
	}
}

// Subscribe 注册客户端通道并返回同时提供清理函数。
func (b *Broker) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 8)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()

	cleanup := func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
		close(ch)
	}
	return ch, cleanup
}

// Publish 将事件广播给所有订阅者。
func (b *Broker) Publish(evt Event) {
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.clients {
		select {
		case ch <- data:
		default:
			// 如果订阅者处理过慢则丢弃消息，避免阻塞。
		}
	}
}
