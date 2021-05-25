package types

import (
	"context"
	"k8s.io/apimachinery/pkg/labels"
	"net"
	"os"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v12 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/clientcmd"
)

const VpcLabel = "vpc.id"
const VpcInternalIPAnnotation = "vpc.internal.ip"
const VpcExternalIPAnnotation = "vpc.external.ip"

const masterLabel = "node-role.kubernetes.io/master"

/*
由于使用k8s包会导致cycle引入，所以这里简单实现一个k8s client go，只需要实现nodeLister
*/

var nodeLister v12.NodeLister
var PodLister v12.PodLister

func init() {

	cf, err := clientcmd.BuildConfigFromFlags(os.Getenv("MASTER_URL"), os.Getenv("KUBECONFIG"))
	if err != nil {
		log.WithError(err).Errorf("init node lister failed. ")
		return
	}

	clientSet := kubernetes.NewForConfigOrDie(cf)

	shardFactory := informers.NewSharedInformerFactoryWithOptions(clientSet, time.Hour)

	nodeLister = shardFactory.Core().V1().Nodes().Lister()
	PodLister = shardFactory.Core().V1().Pods().Lister()

	go shardFactory.Start(context.Background().Done())

	shardFactory.WaitForCacheSync(context.Background().Done())
}

func GetNodeVpcConvert(srcIP string) net.IP {
	nodeList, err := nodeLister.List(labels.Everything())
	if err != nil {
		log.WithError(err).Errorln("get node list failed. ")
		return nil
	}
	log.Debugf("get current node list %d. ", len(nodeList))
	for _, n := range nodeList {
		for _, ip := range n.Status.Addresses {
			if ip.Address == srcIP {
				return GetNodeVpcAddr(n.Name)
			}
		}
	}
	return nil
}

func IsMaster(nodeName string) bool {
	selfNode, err := nodeLister.Get(nodeName)
	if err != nil {
		log.WithError(err).Errorf("get self node %s info failed. ", GetName())
		return false
	}

	if selfNode.Labels == nil {
		return false
	}

	if _, ok := selfNode.Labels[masterLabel]; ok {
		return true
	}

	return false
}

func GetNodeVpcAddr(nodeName string) net.IP {
	if nodeLister == nil {
		log.Warningf("node vpc lister is not init, skip. ")
		return nil
	}

	// TODO add vpc lan
	var nextNodeIP net.IP
	selfNode, err := nodeLister.Get(GetName())
	if err != nil {
		log.WithError(err).Errorf("get self node %s info failed. ", GetName())
		return nil
	}
	nextNode, err := nodeLister.Get(nodeName)
	if err != nil {
		log.WithError(err).Errorf("get next node %s info failed. ", nodeName)
		return nil
	}

	if nextNode.Annotations == nil {
		return nil
	}

	selfVpc := ""
	if selfNode.Labels != nil {
		selfVpc = selfNode.Labels[VpcLabel]
	}
	nextVpc := ""
	if nextNode.Labels != nil {
		nextVpc = nextNode.Labels[VpcLabel]
	}

	if selfVpc == nextVpc && nextNode.Annotations[VpcInternalIPAnnotation] != "" {
		nextNodeIP = net.ParseIP(nextNode.Annotations[VpcInternalIPAnnotation]).To4()
		log.Infof("got same vpc to next node[%s], use internal ip %s. ", nextNode.Name, nextNodeIP.String())
		return nextNodeIP
	}
	if selfVpc != nextVpc && nextNode.Annotations[VpcExternalIPAnnotation] != "" {
		nextNodeIP = net.ParseIP(nextNode.Annotations[VpcExternalIPAnnotation]).To4()
		log.Infof("got diff vpc to next node[%s], use external ip %s. ", nextNode.Name, nextNodeIP.String())
		return nextNodeIP
	}
	return nil
}
