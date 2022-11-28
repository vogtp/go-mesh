package mesh

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/suborbital/grav/grav"
	"github.com/vogtp/go-hcl"
)

const (
	msgTypeBroadcast = "mesh.config.broadcast"
	msgTypeReply     = "mesh.config.reply"
)

// Mgr is the mesh manager
type Mgr struct {
	mu             sync.Mutex
	hcl            hcl.Logger
	grav           *grav.Grav
	running        bool
	connectToNew   bool
	checkIntervall time.Duration
	purgeIntervall time.Duration
	bPod           *grav.Pod
	rPod           *grav.Pod
	NodeCfg        *NodeConfig
}

// NodeConfig stores information about the node and its peers
type NodeConfig struct {
	NodeUUID string
	Name     string
	Endpoint string
	LastSeen time.Time
	Peers    map[string]*NodeConfig
}

// New creates a mesh Mgr
func New(g *grav.Grav, cfg *NodeConfig, settings ...Setting) *Mgr {
	if cfg.Peers == nil {
		cfg.Peers = make(map[string]*NodeConfig, 0)
	}
	cfg.Endpoint = removeHTTPPrefix(cfg.Endpoint)
	m := &Mgr{
		hcl:            hcl.New(),
		grav:           g,
		running:        true,
		connectToNew:   true,
		checkIntervall: 5 * time.Minute,
		purgeIntervall: 10 * time.Minute,
		NodeCfg:        cfg,
	}
	for _, s := range settings {
		s(m)
	}
	m.startReceiver()
	go m.startSender()
	return m
}

func (m *Mgr) startSender() {
	i := time.Second * 30
	<-time.After(time.Second)
	for m.running {
		m.updatePeers()
		m.sendBroadcast()
		<-time.After(i)
		if i < m.checkIntervall {
			i *= 2
		} else {
			i = m.checkIntervall
		}
	}
}

func (m *Mgr) updatePeers() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, p := range m.NodeCfg.Peers {
		if time.Since(p.LastSeen) > m.purgeIntervall {
			delete(m.NodeCfg.Peers, k)
		}
	}
}

func (m *Mgr) sendBroadcast() {
	m.NodeCfg.NodeUUID = m.grav.NodeUUID
	d, err := json.Marshal(m.NodeCfg)
	if err != nil {
		m.hcl.Errorf("Cannot marshal node config: %v", err)
		return
	}
	p := m.grav.Connect()
	defer p.Disconnect()
	m.hcl.Debug("Sending mesh broadcast")
	p.Send(grav.NewMsg(msgTypeBroadcast, d))
}

func removeHTTPPrefix(s string) string {
	if strings.HasPrefix(s, "http://") {
		return s[len("http://"):]
	}
	if strings.HasPrefix(s, "https://") {
		return s[len("https://"):]
	}
	return s
}

func (m *Mgr) startReceiver() {
	m.bPod = m.grav.Connect()
	m.rPod = m.grav.Connect()
	go m.bPod.OnType(msgTypeBroadcast, func(msg grav.Message) error {
		ok := m.processMsg(msg)
		if !ok || !m.connectToNew || len(m.NodeCfg.Peers) < 2 {
			return nil
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		d, err := json.Marshal(m.NodeCfg)
		if err != nil {
			m.hcl.Errorf("Cannot marshal node config: %v", err)
			return nil
		}
		p := m.grav.Connect()
		defer p.Disconnect()
		m.hcl.Debug("Reply to message")
		p.ReplyTo(msg, grav.NewMsg(msgTypeReply, d))
		return nil
	})
	m.rPod.OnType(msgTypeReply, func(msg grav.Message) error {
		go m.processMsg(msg)
		return nil
	})
}

func (m *Mgr) processMsg(msg grav.Message) bool {
	cfg := &NodeConfig{Peers: make(map[string]*NodeConfig)}
	if err := json.Unmarshal(msg.Data(), cfg); err != nil {
		m.hcl.Errorf("cannot unmarshal config: %v", err)
		return false
	}
	m.hcl.Tracef("Message size: %v (Peers: %v)", len(msg.Data()), len(cfg.Peers))
	cfg.LastSeen = time.Now()

	for _, p := range cfg.Peers {
		m.processPeer(p)
	}
	return m.processPeer(cfg)
}

func (m *Mgr) processPeer(cfg *NodeConfig) bool {
	if cfg.NodeUUID == m.grav.NodeUUID {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// remove peers of peers (wo do not want a deep recursiv tree)
	for _, p := range cfg.Peers {
		p.Peers = nil
	}
	_, ok := m.NodeCfg.Peers[cfg.NodeUUID]
	if ok {
		m.NodeCfg.Peers[cfg.NodeUUID] = cfg
		m.hcl.Debugf("Node is known: %s %s %s ", cfg.Name, cfg.NodeUUID, cfg.Endpoint)
		return false
	}
	if err := m.connectPeer(cfg); err != nil {
		m.hcl.Warnf("Cannot connect to peer %s (%s): %v", cfg.Name, cfg.Endpoint, err)
		return false
	}
	m.NodeCfg.Peers[cfg.NodeUUID] = cfg
	return true
}

func (m *Mgr) connectPeer(cfg *NodeConfig) error {
	if !m.connectToNew {
		return nil
	}
	m.hcl.Infof("New mesh node: %s %s %s ", cfg.Name, cfg.NodeUUID, cfg.Endpoint)
	cfg.Endpoint = removeHTTPPrefix(cfg.Endpoint)
	return m.grav.ConnectEndpoint(cfg.Endpoint)
}

// HandlerInfo serves a info page
func (m *Mgr) HandlerInfo(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.WriteHeader(http.StatusOK)
	nLen := 20.
	eLen := 20.
	uuids := make([]string, 0, len(m.NodeCfg.Peers))
	for id, p := range m.NodeCfg.Peers {
		nLen = math.Max(nLen, float64(len(p.Name)))
		eLen = math.Max(eLen, float64(len(p.Endpoint)))
		uuids = append(uuids, id)
	}
	nLen += 2.
	eLen += 2.
	frmt := fmt.Sprintf("Self:\n  %%-%vs %%-%vs (connect peers: %%v)\nPeers (%%v):\n", int(nLen), int(eLen))
	fmt.Fprintf(w, frmt, m.NodeCfg.Name, m.NodeCfg.Endpoint, m.connectToNew, len(m.NodeCfg.Peers))
	frmt = fmt.Sprintf("  %%-%vs %%-%vs Last seen: %%v\n", int(nLen), int(eLen))
	sort.Slice(uuids, func(i, j int) bool {
		return m.NodeCfg.Peers[uuids[i]].Name < m.NodeCfg.Peers[uuids[j]].Name
	})
	for _, id := range uuids {
		p := m.NodeCfg.Peers[id]
		fmt.Fprintf(w, frmt, p.Name, p.Endpoint, time.Since(p.LastSeen).Truncate(time.Second))
	}
}

// Stop the mesh
func (m *Mgr) Stop() {
	m.running = false
	if m.bPod != nil {
		m.bPod.Disconnect()
		m.bPod = nil
	}
	if m.rPod != nil {
		m.rPod.Disconnect()
		m.rPod = nil
	}
}
