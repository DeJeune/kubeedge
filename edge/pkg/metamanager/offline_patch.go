package metamanager

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/kubeedge/beehive/pkg/core/model"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

var notreadypodNames []string

func PodcontainerisReady(content []byte) (bool, error) {
	var pod v1.Pod

	// 解析 JSON 内容
	err := json.Unmarshal(content, &pod)
	if err != nil {
		return false, err
	}

	// 遍历 containerStatuses 列表，检查所有容器的 ready 状态
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if !containerStatus.Ready {
			return false, nil
		}
	}

	// 如果所有容器的 ready 状态都为 true，返回 true
	return true, nil
}

func ResourceGetname(fullString string) string {
	parts := strings.Split(fullString, "/")
	Name := parts[len(parts)-1]
	return Name
}

// addPodName 方法，用于将 Pod 名称添加到数组并输出数组内容
func addnotReadyPodName(podName string) {
	// 将 Pod 名称添加到数组
	notreadypodNames = append(notreadypodNames, podName)

	// 输出当前数组中的所有 Pod 名称
	klog.Info("当前本机的故障Pod: ", notreadypodNames)
}

func checkAndUpdatePod(podName string, delete bool) bool {
	// 标志 Pod 是否存在
	found := false

	// 遍历 Pod 名称数组，检查 Pod 是否存在
	for i, pod := range notreadypodNames {
		if pod == podName {
			found = true
			klog.Info("Pod恢复...")
			if delete {
				// 删除 Pod，调整数组
				notreadypodNames = append(notreadypodNames[:i], notreadypodNames[i+1:]...)
			}
			break
		}
	}

	// 输出当前数组中的所有 Pod 名称
	klog.Info("当前本机的故障Pod: ", notreadypodNames)

	return found
}

func (m *metaManager) processPatch_offline(message model.Message) {
	klog.Info("与云连接断开，通过Edgemesh广播进行PodPatch……")
	Podname := ResourceGetname(message.GetResource()) //解析出Podname
	ready, err := PodcontainerisReady([]byte(message.Content.(string)))
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	} //检查消息中的Pod是否Ready
	if ready {
		klog.Info("Pod Ready...")
		Podfound := checkAndUpdatePod(Podname, ready)
		if Podfound {
			endpoints, err := FetchFormattedJSONFromURL("http://127.0.0.1:10550/api/v1/endpoints")
			if err != nil {
				fmt.Println(err)
			}

			filterendpoints, err := filterEndpointsByPodName(endpoints, Podname)
			if err != nil {
				fmt.Println(err)
			}
			msg := model.NewMessage("").
				BuildRouter(m.nodeName, "edgemesh", "pods", "patch").
				FillBody(filterendpoints)
			klog.Infof("Pod is Ready,Send Pod info to Edgemesh:%+v", msg)
			m.Client.SendMessage(*msg)
		}

	} else {
		klog.Info("Pod not Ready...")
		Podfound := checkAndUpdatePod(Podname, ready)
		if !Podfound {
			addnotReadyPodName(Podname)
			msg := model.NewMessage("").
				BuildRouter(m.nodeName, "edgemesh", "pods", "patch").
				FillBody(ResourceGetname(message.GetResource()))
			klog.Infof("Pod not Ready,Send message to Edgemesh:%+v", msg)

			m.Client.SendMessage(*msg)
		}
	}
}

func FetchFormattedJSONFromURL(url string) (string, error) {
	// 发送 GET 请求到指定的 URL
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 读取响应体
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应数据失败: %v", err)
	}

	// 将响应数据解析为 JSON 格式
	var jsonData interface{}
	err = json.Unmarshal(body, &jsonData)
	if err != nil {
		return "", fmt.Errorf("JSON 解码失败: %v", err)
	}

	// 将解析后的 JSON 数据格式化为字符串
	formattedJSON, err := json.MarshalIndent(jsonData, "", "  ")
	if err != nil {
		return "", fmt.Errorf("格式化 JSON 数据失败: %v", err)
	}

	return string(formattedJSON), nil
}

func filterEndpointsByPodName(jsonStr string, targetPodName string) (string, error) {
	var endpointsList v1.EndpointsList
	err := json.Unmarshal([]byte(jsonStr), &endpointsList)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling JSON: %v", err)
	}

	filteredItems := []v1.Endpoints{}

	for _, endpoint := range endpointsList.Items {
		filteredSubsets := []v1.EndpointSubset{}

		for _, subset := range endpoint.Subsets {
			filteredAddresses := []v1.EndpointAddress{}

			for _, address := range subset.Addresses {
				// 只保留指定 podName 的 address
				if address.TargetRef != nil && address.TargetRef.Kind == "Pod" && address.TargetRef.Name == targetPodName {
					filteredAddresses = append(filteredAddresses, address)
				}
			}

			// 如果 filteredAddresses 不为空，将 subset 添加到 filteredSubsets
			if len(filteredAddresses) > 0 {
				subset.Addresses = filteredAddresses
				filteredSubsets = append(filteredSubsets, subset)
			}
		}

		// 如果 filteredSubsets 不为空，将 endpoint 添加到 filteredItems
		if len(filteredSubsets) > 0 {
			endpoint.Subsets = filteredSubsets
			filteredItems = append(filteredItems, endpoint)
		}
	}

	// 更新过滤后的 EndpointsList
	endpointsList.Items = filteredItems

	// 将过滤后的 EndpointsList 序列化回 JSON
	filteredJSON, err := json.MarshalIndent(endpointsList, "", "  ")
	if err != nil {
		return "", fmt.Errorf("error marshalling filtered JSON: %v", err)
	}

	return string(filteredJSON), nil
}
