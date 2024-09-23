package metamanager

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	beehiveContext "github.com/kubeedge/beehive/pkg/core/context"
	"github.com/kubeedge/beehive/pkg/core/model"
	"k8s.io/klog/v2"
)

type UnixSocketClient struct {
	conn        net.Conn
	socketPath  string
	messageChan chan model.Message
}

// Connect 连接到 Unix socket 服务器
func (usc *UnixSocketClient) Connect() error {
	conn, err := net.Dial("unix", usc.socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to Unix socket: %w", err)
	}
	usc.conn = conn
	return nil
}

// SendMessage 发送消息到 Unix socket 服务器
func (usc *UnixSocketClient) SendMessage(msg model.Message) error {
	if usc.conn == nil {
		return fmt.Errorf("connection is not established")
	}
	data, _ := json.Marshal(msg)
	_, err := usc.conn.Write(data)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// ReceiveMessage 从 Unix socket 服务器接收消息
func (usc *UnixSocketClient) ReceiveMessage(ctx context.Context, errChan chan<- error) {
	defer close(errChan)
	if usc.conn == nil {
		errChan <- fmt.Errorf("connection is not established")
		return
	}
	tempBuf := make([]byte, 1024)
	var buf []byte

	for {
		select {
		case <-ctx.Done():
			return
		default:
			if err := usc.conn.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
				errChan <- fmt.Errorf("failed to set read deadline: %w", err)
				return
			}

			n, err := usc.conn.Read(tempBuf)
			if err != nil {
				if err == io.EOF {
					klog.Info("Connection closed by server")
					errChan <- fmt.Errorf("connection closed by server")
					usc.conn.Close()
					return
				}
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					klog.V(6).Info("Read timeout, retrying")
					continue
				}
				errChan <- fmt.Errorf("failed to read message: %w", err)
				return
			}

			buf = append(buf, tempBuf[:n]...)
			msgs, remaining := usc.parseMessages(buf)
			buf = remaining

			for _, msg := range msgs {
				select {
				case usc.messageChan <- msg:
				default:
					klog.Warning("Message channel is full, dropping message")
				}
			}
		}
	}
}

func (usc *UnixSocketClient) parseMessages(data []byte) ([]model.Message, []byte) {
	var msgs []model.Message
	var start, braceCount int
	var inString bool // 用于处理字符串中的括号
	var escape bool   // 用于处理转义字符

	for i, b := range data {
		switch b {
		case '{':
			// 如果不在字符串中，遇到左大括号则计数增加
			if !inString {
				if braceCount == 0 {
					start = i // 记录每个完整 JSON 对象的起始位置
				}
				braceCount++
			}
		case '}':
			// 如果不在字符串中，遇到右大括号则计数减少
			if !inString {
				braceCount--
				if braceCount == 0 {
					// 找到匹配的完整 JSON 对象
					var msg model.Message
					if err := json.Unmarshal(data[start:i+1], &msg); err == nil {
						msgs = append(msgs, msg)
						// 更新下一个解析的起点
						start = i + 1
					}
				}
			}
		case '"':
			if !escape { // 如果没有被转义
				inString = !inString // 切换字符串状态
			}
		case '\\':
			escape = !escape // 切换转义状态
		default:
			escape = false // 重置转义状态
		}
	}

	// 返回已解析的消息和剩余未解析的数据
	return msgs, data[start:]
}

// Close 关闭连接
func (usc *UnixSocketClient) Close() error {
	if usc.conn == nil {
		return fmt.Errorf("connection is not established")
	}
	return usc.conn.Close()
}

func (m *metaManager) runSocketClient(ctx context.Context) {
	for {
		if err := m.connectAndRun(ctx); err != nil {
			klog.Errorf("Socket client error: %v, retrying in 5 seconds", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}
	}
}

func (m *metaManager) connectAndRun(ctx context.Context) error {
	if err := m.Client.Connect(); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer m.Client.Close()

	klog.Info("Socket client connected successfully on: %s", m.Client.conn.(*net.UnixConn).LocalAddr().String())

	errChan := make(chan error, 1)
	go m.Client.ReceiveMessage(ctx, errChan)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errChan:
			return err
		case msg := <-m.Client.messageChan:
			m.handleEdgeMeshMessage(msg)
		}
	}
}

func (m *metaManager) handleEdgeMeshMessage(msg model.Message) {
	content, ok := msg.GetContent().(map[string]interface{})

	if ok && isObjectEvent(content) {
		klog.Infof("node event: %s %s ", content["eventName"], content["nodeName"])
		return
	}
	beehiveContext.Send(m.Name(), msg)
}

func isObjectEvent(data map[string]interface{}) bool {
	_, ok := data["eventName"]
	if !ok {
		return false
	}
	return ok

}

// func updateNodeStatus(nodeEvent event.NodeEvent, msg model.Message, isReady bool) {
// 	var newCondition v1.NodeCondition
// 	time := metav1.NewTime(nodeEvent.NodeEventInfo.LastSeen)
// 	if isReady {
// 		newCondition = v1.NodeCondition{
// 			Type:               v1.NodeReady,
// 			Status:             v1.ConditionTrue,
// 			LastHeartbeatTime:  time,
// 			LastTransitionTime: time,
// 			Reason:             "EdgeMeshNodeReady",
// 			Message:            "Node is ready in EdgeMesh network",
// 		}
// 	} else {
// 		newCondition = v1.NodeCondition{
// 			Type:               v1.NodeReady,
// 			Status:             v1.ConditionFalse,
// 			LastHeartbeatTime:  time,
// 			LastTransitionTime: time,
// 			Reason:             "EdgeMeshNodeNotReady",
// 			Message:            "Node is not ready in EdgeMesh network",
// 		}
// 	}
// 	cli := client.New()
// 	ns := edgeapi.NodeStatusRequest{}
// 	oldNode, err := cli.Nodes(v1.NamespaceDefault).Get(nodeEvent.NodeEventInfo.NodeName)
// 	if err != nil {
// 		klog.Errorf("Failed to get node %s: %v", nodeEvent.NodeEventInfo.NodeName, err)
// 		return
// 	}
// 	ns.Status.NodeInfo = oldNode.Status.NodeInfo
// 	ns.Status.Addresses = oldNode.Status.Addresses
// 	ns.Status.Capacity = oldNode.Status.Capacity
// 	ns.Status.Allocatable = oldNode.Status.Allocatable
// 	ns.Status.Conditions = append(oldNode.Status.Conditions, newCondition)

// 	err = cli.NodeStatus(metav1.NamespaceDefault).Update(nodeEvent.NodeEventInfo.NodeName, ns)
// 	if err != nil {
// 		klog.Errorf("Failed to update node status for %s: %v", nodeEvent.NodeEventInfo.NodeName, err)
// 		return
// 	}
// }
