package policy

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/operator/gpu-isolation-operator/api/v1alpha1"
)

func policyScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = platformv1alpha1.AddToScheme(s)
	return s
}

func TestResolveActivePolicy_ByLabel(t *testing.T) {
	active := &platformv1alpha1.GPUIsolationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "active",
			Labels: map[string]string{platformv1alpha1.LabelActivePolicy: "true"},
		},
	}
	other := &platformv1alpha1.GPUIsolationPolicy{ObjectMeta: metav1.ObjectMeta{Name: "other"}}
	c := fake.NewClientBuilder().WithObjects(active, other).WithScheme(policyScheme()).Build()

	got, err := ResolveActivePolicy(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "active" {
		t.Fatalf("got %q", got.Name)
	}
}

func TestResolveActivePolicy_SingleCR(t *testing.T) {
	only := &platformv1alpha1.GPUIsolationPolicy{ObjectMeta: metav1.ObjectMeta{Name: "only"}}
	c := fake.NewClientBuilder().WithObjects(only).WithScheme(policyScheme()).Build()
	got, err := ResolveActivePolicy(context.Background(), c)
	if err != nil || got.Name != "only" {
		t.Fatalf("err=%v name=%q", err, got.Name)
	}
}
