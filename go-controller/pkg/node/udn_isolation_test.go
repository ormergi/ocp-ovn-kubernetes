package node

import (
	"context"
	"fmt"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"net"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/knftables"

	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/config"
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/controller"
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/factory"
	networkAttachDefController "github.com/ovn-org/ovn-kubernetes/go-controller/pkg/network-attach-def-controller"
	nodenft "github.com/ovn-org/ovn-kubernetes/go-controller/pkg/node/nftables"
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/testing/nad"
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/types"
	"github.com/ovn-org/ovn-kubernetes/go-controller/pkg/util"
)

var _ = Describe("UDN Host isolation", func() {
	var (
		manager       *UDNHostIsolationManager
		nadController *networkAttachDefController.NetAttachDefinitionController
		wf            *factory.WatchFactory
		fakeClient    *util.OVNNodeClientset
		nft           *knftables.Fake
	)

	const (
		nadNamespace     = "nad-namespace"
		defaultNamespace = "default-namespace"
	)

	getExpectedDump := func(v4ips, v6ips []string) string {
		result :=
			`add table inet ovn-kubernetes
add chain inet ovn-kubernetes udn-isolation { type filter hook output priority 0 ; comment "Host isolation for user defined networks" ; }
add set inet ovn-kubernetes udn-pod-default-ips-v4 { type ipv4_addr ; comment "default network IPs of pods in user defined networks (IPv4)" ; }
add set inet ovn-kubernetes udn-pod-default-ips-v6 { type ipv6_addr ; comment "default network IPs of pods in user defined networks (IPv6)" ; }
add rule inet ovn-kubernetes udn-isolation socket cgroupv2 level 2 kubelet.slice/kubelet.service ip daddr @udn-pod-default-ips-v4 accept
add rule inet ovn-kubernetes udn-isolation ip daddr @udn-pod-default-ips-v4 drop
add rule inet ovn-kubernetes udn-isolation socket cgroupv2 level 2 kubelet.slice/kubelet.service ip6 daddr @udn-pod-default-ips-v6 accept
add rule inet ovn-kubernetes udn-isolation ip6 daddr @udn-pod-default-ips-v6 drop
`
		for _, ip := range v4ips {
			result += fmt.Sprintf("add element inet ovn-kubernetes udn-pod-default-ips-v4 { %s }\n", ip)
		}
		for _, ip := range v6ips {
			result += fmt.Sprintf("add element inet ovn-kubernetes udn-pod-default-ips-v6 { %s }\n", ip)
		}
		return result
	}

	start := func(objects ...runtime.Object) {
		fakeClient = util.GetOVNClientset(objects...).GetNodeClientset()
		var err error
		wf, err = factory.NewNodeWatchFactory(fakeClient, "node1")
		Expect(err).NotTo(HaveOccurred())

		testNCM := &nad.FakeNetworkControllerManager{}
		nadController, err = networkAttachDefController.NewNetAttachDefinitionController("test", testNCM, wf, nil)
		Expect(err).NotTo(HaveOccurred())

		manager = NewUDNHostIsolationManager(true, true, wf.PodCoreInformer(), nadController)

		err = wf.Start()
		Expect(err).NotTo(HaveOccurred())
		err = nadController.Start()
		Expect(err).NotTo(HaveOccurred())

		// Copy manager.Start() sequence, but using fake nft and without running systemd tracker
		manager.kubeletCgroupPath = "kubelet.slice/kubelet.service"
		nft = nodenft.SetFakeNFTablesHelper()
		manager.nft = nft
		err = manager.setupUDNIsolationFromHost()
		Expect(err).NotTo(HaveOccurred())
		err = controller.StartWithInitialSync(manager.podInitialSync, manager.podController)
		Expect(err).NotTo(HaveOccurred())
	}

	BeforeEach(func() {
		config.PrepareTestConfig()
		config.OVNKubernetesFeature.EnableNetworkSegmentation = true
		config.OVNKubernetesFeature.EnableMultiNetwork = true
		config.IPv4Mode = true
		config.IPv6Mode = true

		wf = nil
		manager = nil
		nadController = nil
	})

	AfterEach(func() {
		if wf != nil {
			wf.Shutdown()
		}
		if manager != nil {
			manager.Stop()
		}
		if nadController != nil {
			nadController.Stop()
		}
	})

	It("correctly generates initial rules", func() {
		start()
		Expect(nft.Dump()).To(Equal(getExpectedDump(nil, nil)))
	})

	Context("updates pod IPs", func() {
		It("on restart", func() {
			start(
				newPodWithIPs(nadNamespace, "pod1", true, []string{"1.1.1.1", "2014:100:200::1"}),
				newPodWithIPs(nadNamespace, "pod2", true, []string{"1.1.1.2"}),
				newPodWithIPs(defaultNamespace, "pod3", false, []string{"1.1.1.3"}))
			err := nodenft.MatchNFTRules(getExpectedDump([]string{"1.1.1.1", "1.1.1.2"}, []string{"2014:100:200::1"}), nft.Dump())
			Expect(err).NotTo(HaveOccurred())
		})

		It("on pod add", func() {
			start(
				newPodWithIPs(nadNamespace, "pod1", true, []string{"1.1.1.1", "2014:100:200::1"}))
			err := nodenft.MatchNFTRules(getExpectedDump([]string{"1.1.1.1"}, []string{"2014:100:200::1"}), nft.Dump())
			Expect(err).NotTo(HaveOccurred())
			_, err = fakeClient.KubeClient.CoreV1().Pods(nadNamespace).Create(context.TODO(),
				newPodWithIPs(nadNamespace, "pod2", true, []string{"1.1.1.2", "2014:100:200::2"}), metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() error {
				return nodenft.MatchNFTRules(getExpectedDump([]string{"1.1.1.1", "1.1.1.2"}, []string{"2014:100:200::1", "2014:100:200::2"}), nft.Dump())
			}).Should(Succeed())
			_, err = fakeClient.KubeClient.CoreV1().Pods(defaultNamespace).Create(context.TODO(),
				newPodWithIPs(defaultNamespace, "pod3", false, []string{"1.1.1.3", "2014:100:200::3"}), metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			Consistently(func() error {
				return nodenft.MatchNFTRules(getExpectedDump([]string{"1.1.1.1", "1.1.1.2"}, []string{"2014:100:200::1", "2014:100:200::2"}), nft.Dump())
			}).Should(Succeed())
		})

		It("on pod delete", func() {
			start(
				newPodWithIPs(nadNamespace, "pod1", true, []string{"1.1.1.1", "2014:100:200::1"}),
				newPodWithIPs(nadNamespace, "pod2", true, []string{"1.1.1.2", "2014:100:200::2"}),
				newPodWithIPs(defaultNamespace, "pod3", false, []string{"1.1.1.2"}))
			err := nodenft.MatchNFTRules(getExpectedDump([]string{"1.1.1.1", "1.1.1.2"}, []string{"2014:100:200::1", "2014:100:200::2"}), nft.Dump())
			Expect(err).NotTo(HaveOccurred())
			err = fakeClient.KubeClient.CoreV1().Pods(defaultNamespace).Delete(context.TODO(), "pod3", metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			Consistently(func() error {
				return nodenft.MatchNFTRules(getExpectedDump([]string{"1.1.1.1", "1.1.1.2"}, []string{"2014:100:200::1", "2014:100:200::2"}), nft.Dump())
			}).Should(Succeed())

			err = fakeClient.KubeClient.CoreV1().Pods(nadNamespace).Delete(context.TODO(), "pod2", metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() error {
				return nodenft.MatchNFTRules(getExpectedDump([]string{"1.1.1.1"}, []string{"2014:100:200::1"}), nft.Dump())
			}).Should(Succeed())
		})
	})

})

// newPodWithIPs creates a new pod with the given IPs, only filled for default network.
func newPodWithIPs(namespace, name string, primaryUDN bool, ips []string) *v1.Pod {
	annoPodIPs := make([]string, len(ips))
	for i, ip := range ips {
		if net.ParseIP(ip).To4() != nil {
			annoPodIPs[i] = "\"" + ip + "/24\""
		} else {
			annoPodIPs[i] = "\"" + ip + "/64\""
		}
	}
	annotations := make(map[string]string)
	role := types.NetworkRolePrimary
	if primaryUDN {
		role = types.NetworkRoleInfrastructure
	}
	annotations[util.OvnPodAnnotationName] = fmt.Sprintf(`{"default": {"role": "%s", "ip_addresses":[%s], "mac_address":"0a:58:0a:f4:02:03"}}`,
		role, strings.Join(annoPodIPs, ","))

	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			UID:         ktypes.UID(name),
			Namespace:   namespace,
			Annotations: annotations,
		},
	}
}
