package collectors

import (
	"github.com/openshift/ocs-operator/metrics/internal/options"
	"github.com/prometheus/client_golang/prometheus"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	cephv1listers "github.com/rook/rook/pkg/client/listers/ceph.rook.io/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

var _ prometheus.Collector = &CephClusterCollector{}

// CephClusterCollector is a custom collector for CephCluster Custom Resource
type CephClusterCollector struct {
	CephHealthStatus  *prometheus.Desc
	Informer          cache.SharedIndexInformer
	AllowedNamespaces []string
}

// NewCephClusterCollector constructs a collector
func NewCephClusterCollector(opts *options.Options) *CephClusterCollector {
	// component within the project/exporter
	subsystem := "ceph"
	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Error(err)
	}

	lw := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephclusters", metav1.NamespaceAll, fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &cephv1.CephCluster{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	return &CephClusterCollector{
		CephHealthStatus: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "health_checks"),
			`Health checks in Ceph`,
			[]string{"name", "namespace", "type", "severity"},
			nil,
		),
		Informer:          sharedIndexInformer,
		AllowedNamespaces: opts.AllowedNamespaces,
	}
}

// Run starts CephCluster informer
func (c *CephClusterCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

// Describe implements prometheus.Collector interface
func (c *CephClusterCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.CephHealthStatus,
	}

	for _, d := range ds {
		ch <- d
	}
}

// Collect implements prometheus.Collector interface
func (c *CephClusterCollector) Collect(ch chan<- prometheus.Metric) {
	cephClusterLister := cephv1listers.NewCephClusterLister(c.Informer.GetIndexer())
	cephObjectStores := getAllCephClusters(cephClusterLister, c.AllowedNamespaces)

	if len(cephObjectStores) > 0 {
		c.collectCephHealthChecks(cephObjectStores, ch)
	}
}

func getAllCephClusters(lister cephv1listers.CephClusterLister, namespaces []string) (cephClusters []*cephv1.CephCluster) {
	var tempCephClusters []*cephv1.CephCluster
	var err error
	if len(namespaces) == 0 {
		cephClusters, err = lister.List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephClusters. %v", err)
		}
		return
	}
	for _, namespace := range namespaces {
		tempCephClusters, err = lister.CephClusters(namespace).List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephClusters in namespace %s. %v", namespace, err)
			continue
		}
		cephClusters = append(cephClusters, tempCephClusters...)
	}
	return
}

func (c *CephClusterCollector) collectCephHealthChecks(cephClusters []*cephv1.CephCluster, ch chan<- prometheus.Metric) {
	for _, cephCluster := range cephClusters {
		for checkType, v := range cephCluster.Status.CephStatus.Details {
			ch <- prometheus.MustNewConstMetric(c.CephHealthStatus,
				prometheus.GaugeValue, 1,
				cephCluster.Name,
				cephCluster.Namespace,
				checkType,
				v.Severity,
			)
		}
	}
}
