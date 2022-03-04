package upgrade

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/openshift-psap/special-resource-operator/pkg/cluster"
	"github.com/openshift-psap/special-resource-operator/pkg/registry"
)

func TestPkgUpgrade(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Upgrade Suite")
}

// fakeLayer is a fake struct implementing github.com/google/go-containerregistry/pkg/v1.Layer interface
// The fake does not contain any logic because it's not directly accessed by the clusterInfo object,
// only handled using registry.Registry which is mocked.
type fakeLayer struct{}

func (fk *fakeLayer) Digest() (v1.Hash, error) {
	return v1.Hash{}, fmt.Errorf("not implemented")
}
func (fk *fakeLayer) DiffID() (v1.Hash, error) {
	return v1.Hash{}, fmt.Errorf("not implemented")
}
func (fk *fakeLayer) Compressed() (io.ReadCloser, error) {
	return nil, fmt.Errorf("not implemented")
}
func (fk *fakeLayer) Uncompressed() (io.ReadCloser, error) {
	return nil, fmt.Errorf("not implemented")
}
func (fk *fakeLayer) Size() (int64, error) {
	return 0, fmt.Errorf("not implemented")
}
func (fk *fakeLayer) MediaType() (types.MediaType, error) {
	return types.OCILayer, fmt.Errorf("not implemented")
}

var _ = Describe("ClusterInfo", func() {
	var (
		mockCtrl     *gomock.Controller
		mockRegistry *registry.MockRegistry
		mockCluster  *cluster.MockCluster
		clusterInfo  ClusterInfo
		nodesList    corev1.NodeList
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockRegistry = registry.NewMockRegistry(mockCtrl)
		mockCluster = cluster.NewMockCluster(mockCtrl)
		clusterInfo = NewClusterInfo(mockRegistry, mockCluster)
		nodesList = corev1.NodeList{}
		nodesList.Items = []corev1.Node{}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	type testInput struct {
		nodeInfo       []corev1.NodeSystemInfo
		clusterVersion string
		dtkImages      []string
		dtk            *registry.DriverToolkitEntry
	}

	const (
		kernel         = "4.18.0-305.19.1.el8_4.x86_64"
		kernelRT       = "4.18.0-305.19.1.rt7.91.el8_4.x86_64"
		system         = "rhel"
		systemMajor    = "8"
		systemMinor    = "4"
		clusterVersion = "4.9"
		// first two digits in the last section must match clusterVersion without dots.
		osVersion = "Red Hat Enterprise Linux CoreOS 49.%s%s.202201102104-0 (Ootpa)"
	)

	dtkImages := []string{"quay.io/dtk-image/dtk@sha256:1234567890abcdef"}
	clusterDTK := &registry.DriverToolkitEntry{
		ImageURL:            "",
		KernelFullVersion:   kernel,
		RTKernelFullVersion: kernelRT,
		OSVersion:           fmt.Sprintf("%s.%s", systemMajor, systemMinor),
	}

	Context("has all required data (happy flow)", func() {

		nodeInfoRTKernel := corev1.NodeSystemInfo{
			KernelVersion: kernelRT,
			OSImage:       fmt.Sprintf(osVersion, systemMajor, systemMinor),
		}
		nodeInfoRegularKernel := corev1.NodeSystemInfo{
			KernelVersion: kernel,
			OSImage:       fmt.Sprintf(osVersion, systemMajor, systemMinor),
		}

		DescribeTable("returns information for", func(input testInput, testExpects map[string]NodeVersion) {
			for i, nodeInfo := range input.nodeInfo {
				nodesList.Items = append(nodesList.Items, corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("node-%d", i),
					},
					Status: corev1.NodeStatus{
						NodeInfo: nodeInfo,
					},
				})
			}

			ctx := context.TODO()

			mockCluster.EXPECT().GetDTKImages(ctx).Return(input.dtkImages, nil)
			mockRegistry.EXPECT().LastLayer(ctx, input.dtkImages[0]).Return(&fakeLayer{}, nil)
			mockRegistry.EXPECT().ExtractToolkitRelease(gomock.Any()).Return(input.dtk, nil)

			m, err := clusterInfo.GetClusterInfo(ctx, &nodesList)

			Expect(err).ToNot(HaveOccurred())

			for expectedKernel, expectedNodeVersion := range testExpects {
				Expect(m).To(HaveKeyWithValue(expectedKernel, expectedNodeVersion))
			}
		},
			Entry(
				"1 node with RT kernel",
				testInput{
					nodeInfo:       []corev1.NodeSystemInfo{nodeInfoRTKernel},
					clusterVersion: clusterVersion,
					dtkImages:      dtkImages,
					dtk:            clusterDTK,
				},
				map[string]NodeVersion{
					kernelRT: {
						OSVersion:      fmt.Sprintf("%s.%s", systemMajor, systemMinor),
						OSMajor:        fmt.Sprintf("%s%s", system, systemMajor),
						OSMajorMinor:   fmt.Sprintf("%s%s.%s", system, systemMajor, systemMinor),
						ClusterVersion: clusterVersion,
						DriverToolkit: registry.DriverToolkitEntry{
							ImageURL:            dtkImages[0],
							KernelFullVersion:   kernel,
							RTKernelFullVersion: kernelRT,
							OSVersion:           fmt.Sprintf("%s.%s", systemMajor, systemMinor),
						},
					},
				},
			),

			Entry(
				"1 node with non-RT kernel",
				testInput{
					nodeInfo:       []corev1.NodeSystemInfo{nodeInfoRegularKernel},
					clusterVersion: clusterVersion,
					dtkImages:      dtkImages,
					dtk:            clusterDTK,
				},
				map[string]NodeVersion{
					kernel: {
						OSVersion:      fmt.Sprintf("%s.%s", systemMajor, systemMinor),
						OSMajor:        fmt.Sprintf("%s%s", system, systemMajor),
						OSMajorMinor:   fmt.Sprintf("%s%s.%s", system, systemMajor, systemMinor),
						ClusterVersion: clusterVersion,
						DriverToolkit: registry.DriverToolkitEntry{
							ImageURL:            dtkImages[0],
							KernelFullVersion:   kernel,
							RTKernelFullVersion: kernelRT,
							OSVersion:           fmt.Sprintf("%s.%s", systemMajor, systemMinor),
						},
					},
				},
			),

			Entry(
				"2 nodes: 1st with RT, 2nd with non-RT kernel",
				testInput{
					nodeInfo:       []corev1.NodeSystemInfo{nodeInfoRTKernel, nodeInfoRegularKernel},
					clusterVersion: clusterVersion,
					dtkImages:      dtkImages,
					dtk:            clusterDTK,
				},
				map[string]NodeVersion{
					kernel: {
						OSVersion:      fmt.Sprintf("%s.%s", systemMajor, systemMinor),
						OSMajor:        fmt.Sprintf("%s%s", system, systemMajor),
						OSMajorMinor:   fmt.Sprintf("%s%s.%s", system, systemMajor, systemMinor),
						ClusterVersion: clusterVersion,
						DriverToolkit: registry.DriverToolkitEntry{
							ImageURL:            dtkImages[0],
							KernelFullVersion:   kernel,
							RTKernelFullVersion: kernelRT,
							OSVersion:           fmt.Sprintf("%s.%s", systemMajor, systemMinor),
						},
					},
					kernelRT: {
						OSVersion:      fmt.Sprintf("%s.%s", systemMajor, systemMinor),
						OSMajor:        fmt.Sprintf("%s%s", system, systemMajor),
						OSMajorMinor:   fmt.Sprintf("%s%s.%s", system, systemMajor, systemMinor),
						ClusterVersion: clusterVersion,
						DriverToolkit: registry.DriverToolkitEntry{
							ImageURL:            dtkImages[0],
							KernelFullVersion:   kernel,
							RTKernelFullVersion: kernelRT,
							OSVersion:           fmt.Sprintf("%s.%s", systemMajor, systemMinor),
						},
					},
				},
			),
		)
	})

	Context("lacks some required data/is mismatched", func() {
		const (
			badSystemMinor = "0"
			badKernel      = "someotherkernel"
		)
		badNodeInfoRTKernel := corev1.NodeSystemInfo{
			KernelVersion: kernelRT,
			OSImage:       fmt.Sprintf(osVersion, systemMajor, badSystemMinor),
		}
		badNodeInfoRegularKernel := corev1.NodeSystemInfo{
			KernelVersion: kernel,
			OSImage:       fmt.Sprintf(osVersion, systemMajor, badSystemMinor),
		}
		badNodeInfoNoKernelMatch := corev1.NodeSystemInfo{
			KernelVersion: badKernel,
			OSImage:       fmt.Sprintf(osVersion, systemMajor, systemMinor),
		}

		DescribeTable("returns information for", func(input testInput, testExpects error) {
			for i, nodeInfo := range input.nodeInfo {
				nodesList.Items = append(nodesList.Items, corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("node-%d", i),
					},
					Status: corev1.NodeStatus{
						NodeInfo: nodeInfo,
					},
				})
			}

			ctx := context.TODO()

			mockCluster.EXPECT().GetDTKImages(ctx).Return(input.dtkImages, nil)
			mockRegistry.EXPECT().LastLayer(ctx, input.dtkImages[0]).Return(&fakeLayer{}, nil)
			mockRegistry.EXPECT().ExtractToolkitRelease(gomock.Any()).Return(input.dtk, nil)

			m, err := clusterInfo.GetClusterInfo(ctx, &nodesList)
			Expect(m).To(BeNil())

			Expect(err).Should(MatchError(testExpects))
		},
			Entry(
				"Mismatched OS with regular kernel",
				testInput{
					nodeInfo:       []corev1.NodeSystemInfo{badNodeInfoRegularKernel},
					clusterVersion: clusterVersion,
					dtkImages:      dtkImages,
					dtk:            clusterDTK,
				},
				fmt.Errorf("OSVersion mismatch Node: %s.%s vs. DTK: %s.%s", systemMajor, badSystemMinor, systemMajor, systemMinor),
			),
			Entry(
				"Mismatched OS with RT kernel",
				testInput{
					nodeInfo:       []corev1.NodeSystemInfo{badNodeInfoRTKernel},
					clusterVersion: clusterVersion,
					dtkImages:      dtkImages,
					dtk:            clusterDTK,
				},
				fmt.Errorf("OSVersion mismatch Node: %s.%s vs. DTK: %s.%s", systemMajor, badSystemMinor, systemMajor, systemMinor),
			),
			Entry(
				"Mismatched kernel between nodes and DTK",
				testInput{
					nodeInfo:       []corev1.NodeSystemInfo{badNodeInfoNoKernelMatch},
					clusterVersion: clusterVersion,
					dtkImages:      dtkImages,
					dtk:            clusterDTK,
				},
				fmt.Errorf("DTK kernel not found running in the cluster. kernelFullVersion: %s. rtKernelFullVersion: %s", kernel, kernelRT),
			),
		)
	})

	Context("uses cache", func() {
		It("retrieving from cache after first seeing a new version", func() {
			nodeInfoRegularKernel := corev1.NodeSystemInfo{
				KernelVersion: kernel,
				OSImage:       fmt.Sprintf(osVersion, systemMajor, systemMinor),
			}
			expects := map[string]NodeVersion{
				kernel: {
					OSVersion:      fmt.Sprintf("%s.%s", systemMajor, systemMinor),
					OSMajor:        fmt.Sprintf("%s%s", system, systemMajor),
					OSMajorMinor:   fmt.Sprintf("%s%s.%s", system, systemMajor, systemMinor),
					ClusterVersion: clusterVersion,
					DriverToolkit: registry.DriverToolkitEntry{
						ImageURL:            dtkImages[0],
						KernelFullVersion:   kernel,
						RTKernelFullVersion: kernelRT,
						OSVersion:           fmt.Sprintf("%s.%s", systemMajor, systemMinor),
					},
				},
			}

			for i, nodeInfo := range []corev1.NodeSystemInfo{nodeInfoRegularKernel} {
				nodesList.Items = append(nodesList.Items, corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("node-%d", i),
					},
					Status: corev1.NodeStatus{
						NodeInfo: nodeInfo,
					},
				})
			}

			ctx := context.TODO()

			mockCluster.EXPECT().GetDTKImages(ctx).Return(dtkImages, nil)
			mockRegistry.EXPECT().LastLayer(ctx, dtkImages[0]).Return(&fakeLayer{}, nil)
			mockRegistry.EXPECT().ExtractToolkitRelease(gomock.Any()).Return(clusterDTK, nil)

			m, err := clusterInfo.GetClusterInfo(ctx, &nodesList)

			Expect(err).ToNot(HaveOccurred())
			for expectedKernel, expectedNodeVersion := range expects {
				Expect(m).To(HaveKeyWithValue(expectedKernel, expectedNodeVersion))
			}
			// after first execution it is cached, therefore no more calls to mocks.
			mockCluster.EXPECT().GetDTKImages(ctx).Return(dtkImages, nil)
			mockRegistry.EXPECT().LastLayer(ctx, dtkImages[0]).Times(0)
			mockRegistry.EXPECT().ExtractToolkitRelease(gomock.Any()).Times(0)

			m, err = clusterInfo.GetClusterInfo(ctx, &nodesList)
			Expect(err).ToNot(HaveOccurred())
			for expectedKernel, expectedNodeVersion := range expects {
				Expect(m).To(HaveKeyWithValue(expectedKernel, expectedNodeVersion))
			}
		})
	})
})
