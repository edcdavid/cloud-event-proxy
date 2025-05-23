package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/golang/glog"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/redhat-cne/sdk-go/pkg/subscriber"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// StorageTypeType define storage type
type StorageTypeType string

const (

	// EmptyDir  Default storage type
	EmptyDir StorageTypeType = "EMPTY_DIR"
	// ConfigMap as storage
	ConfigMap StorageTypeType = "CONFIGMAP"
)

// Client has info on how to connect to the kubernetes cluster
type Client struct {
	clientSet kubernetes.Interface
}

// SetClientSet .. set clientset
func (sClient *Client) SetClientSet(c kubernetes.Interface) {
	sClient.clientSet = c
}

// NewClient .. create new client
func NewClient() (*Client, error) {
	var config *rest.Config
	var kubeConfig string
	var err error
	kubeConfig = os.Getenv("KUBECONFIG")
	if kubeConfig != "" {
		glog.V(4).Infof("Loading kube client config from path %q", kubeConfig)
		config, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
		if err != nil {
			return nil, err
		}
	} else {
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	}

	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	return &Client{clientSet: clientset}, err
}

// CreateConfigMap ... create configmap
func (sClient *Client) CreateConfigMap(ctx context.Context, nodeName, namespace string) (cm *corev1.ConfigMap, err error) {
	cm, err = sClient.GetConfigMap(ctx, nodeName, namespace)
	if err == nil {
		log.Infof("ConfigMap %s already exists", cm.Name)
		return cm, nil
	}

	cm = &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeName,
			Namespace: namespace,
		},
		Data: make(map[string]string),
	}

	if cm, err = sClient.clientSet.CoreV1().ConfigMaps(namespace).Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		log.Errorf("Error creating configmap %s", err.Error())
		return
	}
	log.Infof("ConfigMap %s created successfully", cm.Name)
	return
}

// GetConfigMap .. get configmap
func (sClient *Client) GetConfigMap(ctx context.Context, nodeName, namespace string) (*corev1.ConfigMap, error) {
	var cm *corev1.ConfigMap
	var err error
	cm, err = sClient.clientSet.CoreV1().ConfigMaps(namespace).Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return cm, nil
}

// UpdateConfigMap ... update configmap
func (sClient *Client) UpdateConfigMap(ctx context.Context, data []subscriber.Subscriber, nodeName, namespace string) error {
	var cm *corev1.ConfigMap
	var err error
	cm, err = sClient.GetConfigMap(ctx, nodeName, namespace)
	if err != nil {
		if cm, err = sClient.CreateConfigMap(ctx, nodeName, namespace); err != nil {
			log.Errorf("Error fetching configmap %s", err.Error())
			return err
		}
	}

	existingData := cm.Data
	if existingData == nil {
		existingData = make(map[string]string)
	}

	for i := 0; i < len(data); i++ {
		if data[i].Action == channel.DELETE {
			delete(existingData, data[i].ClientID.String())
		} else {
			// Marshal back to json (as original)
			var out []byte
			var e error
			if out, e = json.MarshalIndent(&data[i], "", " "); e != nil {
				log.Errorf("error marshalling subscriber %s", e.Error())
				continue
			}
			log.Infof("updating new subscriber in configmap with following contents %s ", string(out))
			existingData[data[i].ClientID.String()] = string(out)
		}
	}

	cm.Data = existingData
	_, err = sClient.clientSet.CoreV1().ConfigMaps(namespace).Update(ctx, cm, metav1.UpdateOptions{})
	if err != nil {
		log.Errorf("error updating configmap %s", err.Error())
		return err
	}
	log.Info("configmap updated")
	return nil
}

// InitConfigMap ... using configmap
func (sClient *Client) InitConfigMap(storePath, nodeName, namespace string, delay time.Duration, retry int) error {
	var err error
	var cm *corev1.ConfigMap

	for i := 0; i <= retry; i++ {
		cm, err = sClient.CreateConfigMap(context.Background(), nodeName, namespace)
		if err == nil {
			break
		}
		log.Warnf("error creating configmap %s, retrying %d", err.Error(), i)
		time.Sleep(delay)
	}
	if err != nil {
		log.Errorf("failed creating config map %s", err.Error())
		return err
	}

	for clientID, subscriberData := range cm.Data {
		var newSubscriberBytes []byte
		var subscriberErr error
		subscriber := subscriber.Subscriber{}
		if err = json.Unmarshal([]byte(subscriberData), &subscriber); err == nil {
			newSubscriberBytes, subscriberErr = json.MarshalIndent(&subscriber, "", " ")
			if subscriberErr == nil {
				filePath := fmt.Sprintf("%s/%s", storePath, fmt.Sprintf("%s.json", clientID))
				log.Infof("persisting following contents from configmap to file %s: %s\n", filePath, string(newSubscriberBytes))
				if subscriberErr = os.WriteFile(filePath, newSubscriberBytes, 0600); subscriberErr != nil {
					log.Errorf("error writing subscription to a file %s", subscriberErr.Error())
				}
			} else {
				log.Errorf("error marshalling subscriber data: %s", subscriberErr.Error())
				continue
			}
		} else {
			log.Errorf("error unmarshalling data from configmap")
			return err
		}
	}
	return nil
}
