package k8s

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// PeerInfo represents information about a peer SMTP server
type PeerInfo struct {
	PodName   string
	PodIP     string
	NodeName  string
	Region    string
	Zone      string
	Labels    map[string]string
	Ready     bool
	StartTime time.Time
}

// EndpointEvent represents a change in endpoints
type EndpointEvent struct {
	Type     watch.EventType
	Endpoint *corev1.Endpoints
}

// ServiceDiscovery handles Kubernetes service discovery
type ServiceDiscovery struct {
	clientset *kubernetes.Clientset
	namespace string
	selector  string
	logger    *zap.Logger

	peers   map[string]*PeerInfo
	peersMu sync.RWMutex
}

// NewServiceDiscovery creates a new Kubernetes service discovery client
func NewServiceDiscovery(namespace, selector string, logger *zap.Logger) (*ServiceDiscovery, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig for development
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = os.Getenv("HOME") + "/.kube/config"
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build kubernetes config: %w", err)
		}
		logger.Info("Using kubeconfig for development", zap.String("path", kubeconfig))
	} else {
		logger.Info("Using in-cluster Kubernetes configuration")
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	sd := &ServiceDiscovery{
		clientset: clientset,
		namespace: namespace,
		selector:  selector,
		logger:    logger,
		peers:     make(map[string]*PeerInfo),
	}

	return sd, nil
}

// DiscoverPeers discovers all peer SMTP servers in the cluster
func (sd *ServiceDiscovery) DiscoverPeers(ctx context.Context) ([]*PeerInfo, error) {
	pods, err := sd.clientset.CoreV1().Pods(sd.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: sd.selector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	var peers []*PeerInfo
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning {
			ready := isPodReady(&pod)
			peer := &PeerInfo{
				PodName:   pod.Name,
				PodIP:     pod.Status.PodIP,
				NodeName:  pod.Spec.NodeName,
				Region:    pod.Labels["topology.kubernetes.io/region"],
				Zone:      pod.Labels["topology.kubernetes.io/zone"],
				Labels:    pod.Labels,
				Ready:     ready,
				StartTime: pod.Status.StartTime.Time,
			}
			peers = append(peers, peer)

			// Cache the peer
			sd.peersMu.Lock()
			sd.peers[pod.Name] = peer
			sd.peersMu.Unlock()
		}
	}

	sd.logger.Info("Discovered peers", zap.Int("count", len(peers)))
	return peers, nil
}

// GetPeers returns the currently cached peers
func (sd *ServiceDiscovery) GetPeers() []*PeerInfo {
	sd.peersMu.RLock()
	defer sd.peersMu.RUnlock()

	peers := make([]*PeerInfo, 0, len(sd.peers))
	for _, peer := range sd.peers {
		peers = append(peers, peer)
	}
	return peers
}

// GetPeersByRegion returns peers filtered by region
func (sd *ServiceDiscovery) GetPeersByRegion(region string) []*PeerInfo {
	sd.peersMu.RLock()
	defer sd.peersMu.RUnlock()

	var peers []*PeerInfo
	for _, peer := range sd.peers {
		if peer.Region == region && peer.Ready {
			peers = append(peers, peer)
		}
	}
	return peers
}

// WatchPods watches for pod changes and keeps peer list updated
func (sd *ServiceDiscovery) WatchPods(ctx context.Context) error {
	watcher, err := sd.clientset.CoreV1().Pods(sd.namespace).Watch(ctx, metav1.ListOptions{
		LabelSelector: sd.selector,
	})
	if err != nil {
		return fmt.Errorf("failed to create pod watcher: %w", err)
	}

	go func() {
		defer watcher.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.ResultChan():
				if !ok {
					// Watcher closed, need to restart
					sd.logger.Warn("Pod watcher closed, restarting...")
					time.Sleep(5 * time.Second)
					newWatcher, err := sd.clientset.CoreV1().Pods(sd.namespace).Watch(context.Background(), metav1.ListOptions{
						LabelSelector: sd.selector,
					})
					if err != nil {
						sd.logger.Error("Failed to restart pod watcher", zap.Error(err))
						return
					}
					watcher = newWatcher
					continue
				}

				pod, ok := event.Object.(*corev1.Pod)
				if !ok {
					continue
				}

				sd.handlePodEvent(event.Type, pod)
			}
		}
	}()

	return nil
}

// WatchEndpoints watches for endpoint changes
func (sd *ServiceDiscovery) WatchEndpoints(ctx context.Context, serviceName string) (<-chan EndpointEvent, error) {
	events := make(chan EndpointEvent, 10)

	watcher, err := sd.clientset.CoreV1().Endpoints(sd.namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", serviceName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create endpoint watcher: %w", err)
	}

	go func() {
		defer close(events)
		defer watcher.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.ResultChan():
				if !ok {
					return
				}

				endpoint, ok := event.Object.(*corev1.Endpoints)
				if !ok {
					continue
				}

				events <- EndpointEvent{
					Type:     event.Type,
					Endpoint: endpoint,
				}
			}
		}
	}()

	return events, nil
}

// GetCurrentPodInfo returns information about the current pod
func (sd *ServiceDiscovery) GetCurrentPodInfo() (*PeerInfo, error) {
	podName := os.Getenv("POD_NAME")
	if podName == "" {
		return nil, fmt.Errorf("POD_NAME environment variable not set")
	}

	pod, err := sd.clientset.CoreV1().Pods(sd.namespace).Get(context.Background(), podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get current pod: %w", err)
	}

	return &PeerInfo{
		PodName:   pod.Name,
		PodIP:     pod.Status.PodIP,
		NodeName:  pod.Spec.NodeName,
		Region:    pod.Labels["topology.kubernetes.io/region"],
		Zone:      pod.Labels["topology.kubernetes.io/zone"],
		Labels:    pod.Labels,
		Ready:     isPodReady(pod),
		StartTime: pod.Status.StartTime.Time,
	}, nil
}

// GetServiceEndpoints returns the endpoints for a service
func (sd *ServiceDiscovery) GetServiceEndpoints(ctx context.Context, serviceName string) ([]string, error) {
	endpoints, err := sd.clientset.CoreV1().Endpoints(sd.namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get endpoints: %w", err)
	}

	var addresses []string
	for _, subset := range endpoints.Subsets {
		for _, addr := range subset.Addresses {
			for _, port := range subset.Ports {
				addresses = append(addresses, fmt.Sprintf("%s:%d", addr.IP, port.Port))
			}
		}
	}

	return addresses, nil
}

// handlePodEvent processes pod events
func (sd *ServiceDiscovery) handlePodEvent(eventType watch.EventType, pod *corev1.Pod) {
	sd.peersMu.Lock()
	defer sd.peersMu.Unlock()

	switch eventType {
	case watch.Added, watch.Modified:
		if pod.Status.Phase == corev1.PodRunning {
			ready := isPodReady(pod)
			peer := &PeerInfo{
				PodName:   pod.Name,
				PodIP:     pod.Status.PodIP,
				NodeName:  pod.Spec.NodeName,
				Region:    pod.Labels["topology.kubernetes.io/region"],
				Zone:      pod.Labels["topology.kubernetes.io/zone"],
				Labels:    pod.Labels,
				Ready:     ready,
				StartTime: pod.Status.StartTime.Time,
			}
			sd.peers[pod.Name] = peer
			sd.logger.Debug("Pod updated",
				zap.String("name", pod.Name),
				zap.String("ip", pod.Status.PodIP),
				zap.Bool("ready", ready))
		}

	case watch.Deleted:
		delete(sd.peers, pod.Name)
		sd.logger.Info("Pod removed", zap.String("name", pod.Name))
	}
}

// isPodReady checks if a pod is ready
func isPodReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// Close stops all watchers and cleans up
func (sd *ServiceDiscovery) Close() {
	sd.logger.Info("Shutting down service discovery")
	// Watchers are context-based, so they clean up automatically
}
