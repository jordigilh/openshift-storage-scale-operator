package localvolumediscovery

import (
	"context"
	"testing"

	localv1alpha1 "github.com/openshift-storage-scale/openshift-fusion-access-operator/api/v1alpha1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	name          = "auto-discover-devices"
	namespace     = "local-storage"
	hostnameLabel = "kubernetes.io/hostname"
)

var discoveryDaemonSet = &appsv1.DaemonSet{
	ObjectMeta: metav1.ObjectMeta{
		Name:      DeviceFinderDiscovery,
		Namespace: namespace,
	},
	Status: appsv1.DaemonSetStatus{
		NumberReady:            3,
		DesiredNumberScheduled: 3,
	},
}

var mockNodeList = &corev1.NodeList{
	TypeMeta: metav1.TypeMeta{
		Kind: "NodeList",
	},
	Items: []corev1.Node{
		{
			TypeMeta: metav1.TypeMeta{
				Kind: "Node",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "Node1",
				Labels: map[string]string{
					hostnameLabel: "Node1",
				},
			},
		},
		{
			TypeMeta: metav1.TypeMeta{
				Kind: "Node",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "Node2",
				Labels: map[string]string{
					hostnameLabel: "Node2",
				},
			},
		},
	},
}

var localVolumeDiscoveryCR = localv1alpha1.LocalVolumeDiscovery{
	ObjectMeta: metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	},
	TypeMeta: metav1.TypeMeta{
		Kind: "LocalVolumeDiscovery",
	},
	Spec: localv1alpha1.LocalVolumeDiscoverySpec{
		NodeSelector: &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      hostnameLabel,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{"Node1", "Node2"},
					},
				}},
			},
		},
	},
}

var localVolumeDiscoveryResultList = localv1alpha1.LocalVolumeDiscoveryResultList{
	TypeMeta: metav1.TypeMeta{
		Kind: "LocalVolumeDiscoveryResultList",
	},
	Items: []localv1alpha1.LocalVolumeDiscoveryResult{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "discovery-result-node1",
				Namespace: namespace,
			},
			Spec: localv1alpha1.LocalVolumeDiscoveryResultSpec{
				NodeName: "Node1",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "discovery-result-node2",
				Namespace: namespace,
			},
			Spec: localv1alpha1.LocalVolumeDiscoveryResultSpec{
				NodeName: "Node2",
			},
		},
	},
}

func newFakeLocalVolumeDiscoveryReconciler(t *testing.T, objs ...runtime.Object) *LocalVolumeDiscoveryReconciler {
	scheme, err := localv1alpha1.SchemeBuilder.Build()
	assert.NoErrorf(t, err, "creating scheme")

	err = corev1.AddToScheme(scheme)
	assert.NoErrorf(t, err, "adding corev1 to scheme")

	err = appsv1.AddToScheme(scheme)
	assert.NoErrorf(t, err, "adding appsv1 to scheme")

	err = monitoringv1.AddToScheme(scheme)
	assert.NoErrorf(t, err, "adding corev1 to scheme")

	crsWithStatus := []client.Object{
		&localv1alpha1.LocalVolumeDiscovery{},
	}

	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(crsWithStatus...).WithRuntimeObjects(objs...).Build()

	return &LocalVolumeDiscoveryReconciler{
		Client: client,
		Scheme: scheme,
	}
}

func TestDiscoveryReconciler(t *testing.T) {
	testcases := []struct {
		label                        string
		discoveryDaemonCreated       bool
		discoveryDesiredDaemonsCount int32
		discoveryReadyDaemonsCount   int32
		expectedPhase                localv1alpha1.DiscoveryPhase
		conditionType                string
		conditionStatus              metav1.ConditionStatus
	}{
		{
			label:                        "case 1", // all the desired discovery daemonset pods are running
			discoveryDaemonCreated:       true,
			discoveryDesiredDaemonsCount: 1,
			discoveryReadyDaemonsCount:   1,
			expectedPhase:                localv1alpha1.Discovering,
			conditionType:                "Available",
			conditionStatus:              metav1.ConditionTrue,
		},
		{
			label:                        "case 2", // all the desired discovery daemonset pods are running
			discoveryDaemonCreated:       true,
			discoveryDesiredDaemonsCount: 100,
			discoveryReadyDaemonsCount:   100,
			expectedPhase:                localv1alpha1.Discovering,
			conditionType:                "Available",
			conditionStatus:              metav1.ConditionTrue,
		},
		{
			label:                        "case 3", // ready discovery daemonset pods are less than the desired count
			discoveryDaemonCreated:       true,
			discoveryDesiredDaemonsCount: 100,
			discoveryReadyDaemonsCount:   80,
			expectedPhase:                localv1alpha1.Discovering,
			conditionType:                "Progressing",
			conditionStatus:              metav1.ConditionFalse,
		},
		{
			label:                        "case 4", // no discovery daemonset pods are running
			discoveryDaemonCreated:       true,
			discoveryDesiredDaemonsCount: 0,
			discoveryReadyDaemonsCount:   0,
			expectedPhase:                localv1alpha1.DiscoveryFailed,
			conditionType:                "Degraded",
			conditionStatus:              metav1.ConditionFalse,
		},

		{
			label:                        "case 5", // discovery daemonset not created
			discoveryDaemonCreated:       false,
			discoveryDesiredDaemonsCount: 0,
			discoveryReadyDaemonsCount:   0,
			expectedPhase:                localv1alpha1.DiscoveryFailed,
			conditionType:                "Degraded",
			conditionStatus:              metav1.ConditionFalse,
		},
	}

	for _, tc := range testcases {
		discoveryDS := &appsv1.DaemonSet{}
		discoveryDaemonSet.DeepCopyInto(discoveryDS)
		discoveryDS.Status.NumberReady = tc.discoveryReadyDaemonsCount
		discoveryDS.Status.DesiredNumberScheduled = tc.discoveryDesiredDaemonsCount

		discoveryObj := &localv1alpha1.LocalVolumeDiscovery{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			TypeMeta: metav1.TypeMeta{
				Kind: "LocalVolumeDiscovery",
			},
		}
		objects := []runtime.Object{
			discoveryObj,
		}

		if tc.discoveryDaemonCreated {
			objects = append(objects, discoveryDS)
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      discoveryObj.Name,
				Namespace: discoveryObj.Namespace,
			},
		}
		fakeReconciler := newFakeLocalVolumeDiscoveryReconciler(t, objects...)
		_, err := fakeReconciler.Reconcile(context.TODO(), req)
		assert.NoError(t, err, tc.label)
		err = fakeReconciler.Client.Get(context.TODO(), types.NamespacedName{Name: discoveryObj.Name, Namespace: discoveryObj.Namespace}, discoveryObj)
		assert.NoError(t, err)
		assert.Equalf(t, tc.expectedPhase, discoveryObj.Status.Phase, "[%s] invalid phase", tc.label)
		assert.Equalf(t, tc.conditionType, discoveryObj.Status.Conditions[0].Type, "[%s] invalid condition type", tc.label)
		assert.Equalf(t, tc.conditionStatus, discoveryObj.Status.Conditions[0].Status, "[%s] invalid condition status", tc.label)
	}
}

func TestDeleteOrphanDiscoveryResults(t *testing.T) {
	nodeList := &corev1.NodeList{}
	mockNodeList.DeepCopyInto(nodeList)
	discoveryDS := &appsv1.DaemonSet{}
	discoveryDaemonSet.DeepCopyInto(discoveryDS)

	discoveryObj := &localv1alpha1.LocalVolumeDiscovery{}
	localVolumeDiscoveryCR.DeepCopyInto(discoveryObj)

	discoveryResults := &localv1alpha1.LocalVolumeDiscoveryResultList{}
	localVolumeDiscoveryResultList.DeepCopyInto(discoveryResults)

	objects := []runtime.Object{
		nodeList, discoveryObj, discoveryDS, discoveryResults,
	}

	fakeReconciler := newFakeLocalVolumeDiscoveryReconciler(t, objects...)
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      discoveryObj.Name,
			Namespace: discoveryObj.Namespace,
		},
	}

	_, err := fakeReconciler.Reconcile(context.TODO(), req)
	assert.NoError(t, err)
	results := &localv1alpha1.LocalVolumeDiscoveryResultList{}
	err = fakeReconciler.Client.List(context.TODO(), results, client.InNamespace(namespace))
	assert.NoError(t, err)
	assert.Equal(t, 2, len(results.Items))

	// update discovery CR to remove "Node2"
	discoveryObj.Spec.NodeSelector.NodeSelectorTerms[0].MatchExpressions[0].Values = []string{"Node1"}
	fakeReconciler = newFakeLocalVolumeDiscoveryReconciler(t, objects...)
	err = fakeReconciler.deleteOrphanDiscoveryResults(context.TODO(), discoveryObj)
	assert.NoError(t, err)
	// assert that discovery result object on "Node2" is deleted
	results = &localv1alpha1.LocalVolumeDiscoveryResultList{}
	err = fakeReconciler.Client.List(context.TODO(), results, client.InNamespace(namespace))
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results.Items))
	assert.Equal(t, "Node1", results.Items[0].Spec.NodeName)

	// skip deletion of orphan results when no NodeSelector is provided
	discoveryObj.Spec = localv1alpha1.LocalVolumeDiscoverySpec{}
	err = fakeReconciler.deleteOrphanDiscoveryResults(context.TODO(), discoveryObj)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(results.Items))
	assert.Equal(t, "Node1", results.Items[0].Spec.NodeName)
}
