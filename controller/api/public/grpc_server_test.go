package public

import (
	"context"
	"sort"
	"testing"

	"github.com/golang/protobuf/ptypes/duration"
	tap "github.com/linkerd/linkerd2/controller/gen/controller/tap"
	pb "github.com/linkerd/linkerd2/controller/gen/public"
	"github.com/linkerd/linkerd2/controller/k8s"
	"github.com/prometheus/common/model"
)

type listPodsExpected struct {
	err     error
	k8sRes  []string
	promRes model.Value
	res     pb.ListPodsResponse
}

// sort Pods in ListPodResponses for easier comparison
type ByPod []*pb.Pod

func (bp ByPod) Len() int           { return len(bp) }
func (bp ByPod) Swap(i, j int)      { bp[i], bp[j] = bp[j], bp[i] }
func (bp ByPod) Less(i, j int) bool { return bp[i].Name <= bp[j].Name }

func listPodResponsesEqual(a pb.ListPodsResponse, b pb.ListPodsResponse) bool {
	if len(a.Pods) != len(b.Pods) {
		return false
	}

	sort.Sort(ByPod(a.Pods))
	sort.Sort(ByPod(b.Pods))

	for i := 0; i < len(a.Pods); i++ {
		aPod := a.Pods[i]
		bPod := b.Pods[i]

		if (aPod.Name != bPod.Name) ||
			(aPod.Added != bPod.Added) ||
			(aPod.Status != bPod.Status) ||
			(aPod.PodIP != bPod.PodIP) ||
			(aPod.GetDeployment() != bPod.GetDeployment()) {
			return false
		}

		if (aPod.SinceLastReport == nil && bPod.SinceLastReport != nil) ||
			(aPod.SinceLastReport != nil && bPod.SinceLastReport == nil) {
			return false
		}
	}

	return true
}

func TestListPods(t *testing.T) {
	t.Run("Successfully performs a query based on resource type", func(t *testing.T) {
		expectations := []listPodsExpected{
			listPodsExpected{
				err: nil,
				promRes: model.Vector{
					&model.Sample{
						Metric:    model.Metric{"pod": "emojivoto-meshed"},
						Timestamp: 456,
					},
				},
				k8sRes: []string{`
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-meshed
  namespace: emojivoto
  labels:
    pod-template-hash: hash-meshed
  ownerReferences:
  - apiVersion: extensions/v1beta1
    kind: ReplicaSet
    name: rs-emojivoto-meshed
status:
  phase: Running
  podIP: 1.2.3.4
`, `
apiVersion: v1
kind: Pod
metadata:
  name: emojivoto-not-meshed
  namespace: emojivoto
  labels:
    pod-template-hash: hash-not-meshed
  ownerReferences:
  - apiVersion: extensions/v1beta1
    kind: ReplicaSet
    name: rs-emojivoto-not-meshed
status:
  phase: Pending
  podIP: 4.3.2.1
`, `
apiVersion: apps/v1beta2
kind: ReplicaSet
metadata:
  name: rs-emojivoto-meshed
  namespace: emojivoto
  ownerReferences:
  - apiVersion: extensions/v1beta1
    kind: Deployment
    name: meshed-deployment
spec:
  selector:
    matchLabels:
      pod-template-hash: hash-meshed
`, `
apiVersion: apps/v1beta2
kind: ReplicaSet
metadata:
  name: rs-emojivoto-not-meshed
  namespace: emojivoto
  ownerReferences:
  - apiVersion: extensions/v1beta1
    kind: Deployment
    name: not-meshed-deployment
spec:
  selector:
    matchLabels:
      pod-template-hash: hash-not-meshed
`,
				},
				res: pb.ListPodsResponse{
					Pods: []*pb.Pod{
						&pb.Pod{
							Name:            "emojivoto/emojivoto-meshed",
							Added:           true,
							SinceLastReport: &duration.Duration{},
							Status:          "Running",
							PodIP:           "1.2.3.4",
							Owner:           &pb.Pod_Deployment{Deployment: "emojivoto/meshed-deployment"},
						},
						&pb.Pod{
							Name:   "emojivoto/emojivoto-not-meshed",
							Status: "Pending",
							PodIP:  "4.3.2.1",
							Owner:  &pb.Pod_Deployment{Deployment: "emojivoto/not-meshed-deployment"},
						},
					},
				},
			},
		}

		for _, exp := range expectations {
			k8sAPI, err := k8s.NewFakeAPI(exp.k8sRes...)
			if err != nil {
				t.Fatalf("NewFakeAPI returned an error: %s", err)
			}

			fakeGrpcServer := newGrpcServer(
				&MockProm{Res: exp.promRes},
				tap.NewTapClient(nil),
				k8sAPI,
				"linkerd",
				[]string{},
			)

			k8sAPI.Sync(nil)

			rsp, err := fakeGrpcServer.ListPods(context.TODO(), &pb.ListPodsRequest{})
			if err != exp.err {
				t.Fatalf("Expected error: %s, Got: %s", exp.err, err)
			}

			if !listPodResponsesEqual(exp.res, *rsp) {
				t.Fatalf("Expected: %+v, Got: %+v", &exp.res, rsp)
			}
		}
	})
}
