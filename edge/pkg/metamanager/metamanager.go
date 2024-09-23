package metamanager

import (
	"context"
	"os"

	"github.com/beego/beego/orm"
	"k8s.io/klog/v2"

	"github.com/kubeedge/beehive/pkg/core"
	beehiveContext "github.com/kubeedge/beehive/pkg/core/context"
	"github.com/kubeedge/beehive/pkg/core/model"
	"github.com/kubeedge/kubeedge/common/constants"
	"github.com/kubeedge/kubeedge/edge/pkg/common/modules"
	metamanagerconfig "github.com/kubeedge/kubeedge/edge/pkg/metamanager/config"
	"github.com/kubeedge/kubeedge/edge/pkg/metamanager/dao"
	v2 "github.com/kubeedge/kubeedge/edge/pkg/metamanager/dao/v2"
	"github.com/kubeedge/kubeedge/edge/pkg/metamanager/metaserver"
	metaserverconfig "github.com/kubeedge/kubeedge/edge/pkg/metamanager/metaserver/config"
	"github.com/kubeedge/kubeedge/edge/pkg/metamanager/metaserver/kubernetes/storage/sqlite/imitator"
	"github.com/kubeedge/kubeedge/pkg/apis/componentconfig/edgecore/v1alpha2"
)

type metaManager struct {
	enable   bool
	Client   *UnixSocketClient
	nodeName string
}

var _ core.Module = (*metaManager)(nil)

func newMetaManager(enable bool) *metaManager {
	klog.Info("initialize socket client")
	nodeName, _ := os.LookupEnv("NODE_NAME")
	return &metaManager{
		enable: enable,
		Client: &UnixSocketClient{
			socketPath:  constants.DefaultSocketAddr,
			messageChan: make(chan model.Message),
		},
		nodeName: nodeName,
	}
}

// Register register metamanager
func Register(metaManager *v1alpha2.MetaManager) {
	metamanagerconfig.InitConfigure(metaManager)
	meta := newMetaManager(metaManager.Enable)
	initDBTable(meta)
	core.Register(meta)
}

// initDBTable create table
func initDBTable(module core.Module) {
	klog.Infof("Begin to register %v db model", module.Name())
	if !module.Enable() {
		klog.Infof("Module %s is disabled, DB meta for it will not be registered", module.Name())
		return
	}
	orm.RegisterModel(new(dao.Meta))
	orm.RegisterModel(new(v2.MetaV2))
}

func (*metaManager) Name() string {
	return modules.MetaManagerModuleName
}

func (*metaManager) Group() string {
	return modules.MetaGroup
}

func (m *metaManager) Enable() bool {
	return m.enable
}

func (m *metaManager) Start() {
	ctx, cancel := context.WithCancel(beehiveContext.GetContext())
	defer cancel()

	if metaserverconfig.Config.Enable {
		imitator.StorageInit()
		go metaserver.NewMetaServer().Start(ctx.Done())
	}

	klog.Info("Starting meta manager")
	go m.runSocketClient(ctx)
	m.runMetaManager()
}
