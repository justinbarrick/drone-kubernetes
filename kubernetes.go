package kubernetes

import (
  "fmt"
	"io"
	"regexp"

	"github.com/cncd/pipeline/pipeline/backend"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type engine struct {
	namespace string
	client *kubernetes.Clientset
}

func convertName(name string) string {
  regex, _ := regexp.Compile("_")
	return regex.ReplaceAllString(name, "-")
}

// New returns a new Kubernetes Engine.
func New(namespace string, client *kubernetes.Clientset) backend.Engine {
	return &engine{
		namespace: namespace,
		client: client,
	}
}

func NewEnv() (backend.Engine, error) {
	config, err := clientcmd.BuildConfigFromFlags("", "/home/user/kubeconfig")
	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return New("hello", client), nil
}

// Setup the pipeline environment.
func (e *engine) Setup(*backend.Config) error {
	fmt.Println("Creating namespace", e.namespace)

	_, err := e.client.CoreV1().Namespaces().Get(e.namespace, metav1.GetOptions{})
	if err == nil {
		fmt.Println("Namespace already exists!")
		return nil
	}

	_, err = e.client.CoreV1().Namespaces().Create(&v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: e.namespace,
		},
	})

	if err != nil {
		return err
	}

	fmt.Println("Created namespace", e.namespace)
	return nil
}

func toConfig(proc *backend.Step, namespace string) v1.Pod {
	container := v1.Container{
		Name: convertName(proc.Name),
		Image: proc.Image,
		WorkingDir: proc.WorkingDir,
	}

	if len(proc.Environment) != 0 {
		container.Env = []v1.EnvVar{}

		for k, v := range proc.Environment {
			container.Env = append(container.Env, v1.EnvVar{ Name: k, Value: v, })
		}
	}

	if len(proc.Entrypoint) != 0 || len(proc.Command) != 0 {
		container.Command = []string{}

		if len(proc.Entrypoint) != 0 {
			container.Command = append(container.Command, proc.Entrypoint...)
		}

		if len(proc.Command) != 0 {
			container.Command = append(container.Command, proc.Command...)
		}
	}

	return v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name: convertName(proc.Name),
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{container},
			RestartPolicy: "Never",
		},
	}
}

// Start the pipeline step.
func (e *engine) Exec(proc *backend.Step) error {
	// POST /api/v1/namespaces/{namespace}/pods
	pod_config := toConfig(proc, e.namespace)

	fmt.Println("Creating pod in namespace", e.namespace, proc.Name)
	pod, err := e.client.CoreV1().Pods(e.namespace).Create(&pod_config)
	if err != nil {
	  fmt.Println("Failed creating pod in namespace", e.namespace, proc.Name, e)
		return err
	}

	fmt.Println("Waiting for pod to start...")
	pod, _ = e.getPod(proc.Name)
	for pod.Status.Phase == "Pending" {
		pod, _ = e.getPod(proc.Name)
	}


	fmt.Println("Pod created", proc.Name)
	return nil
}

func (e *engine) getPod(name string) (*v1.Pod, error) {
  name = convertName(name)

	return e.client.CoreV1().Pods(e.namespace).Get(name, metav1.GetOptions{})
}

// DEPRECATED
// Kill the pipeline step.
func (e *engine) Kill(proc *backend.Step) error {
	fmt.Println("Kill job!", proc.Name)
	return nil
}

// Wait for the pipeline step to complete and returns
// the completion results.
func (e *engine) Wait(proc *backend.Step) (*backend.State, error) {
	fmt.Println("Wait for job!", proc.Name)

	pod, err := e.getPod(proc.Name)
	if err != nil {
		return nil, err
	}

  for pod.Status.Phase == "Running" {
		pod, err = e.getPod(proc.Name)
		if err != nil {
			return nil, err
		}
	}

	return &backend.State{
		Exited: true,
		ExitCode: int(pod.Status.ContainerStatuses[0].State.Terminated.ExitCode),
		OOMKilled: false,
	}, nil
}

// Tail the pipeline step logs.
func (e *engine) Tail(proc *backend.Step) (io.ReadCloser, error) {
	fmt.Println("Tail job!", proc.Name)

	req := e.client.CoreV1().RESTClient().Get().
		Namespace(e.namespace).
		Name(convertName(proc.Name)).
		Resource("pods").
		SubResource("log").
		Param("follow", "true")

	return req.Stream()
}

// Destroy the pipeline environment.
func (e *engine) Destroy(conf *backend.Config) error {
	fmt.Println("Deleting namespace", e.namespace)

	err := e.client.CoreV1().Namespaces().Delete(e.namespace, &metav1.DeleteOptions{})
	if err != nil {
		return err
	}

	return nil
}
