package v070to080

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/longhorn/longhorn-manager/datastore"
	"github.com/longhorn/longhorn-manager/engineapi"
	"github.com/longhorn/longhorn-manager/types"
	"github.com/longhorn/longhorn-manager/util"

	longhorn "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta1"
	lhclientset "github.com/longhorn/longhorn-manager/k8s/pkg/client/clientset/versioned"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	upgradeLogPrefix = "upgrade from v0.7.0 to v0.8.0: "
)

func migrateEngineBinaries() error {
	return util.CopyHostDirectoryContent(DeprecatedEngineBinaryDirectoryOnHost, types.EngineBinaryDirectoryOnHost)
}

func UpgradeLocalNode() error {
	if err := migrateEngineBinaries(); err != nil {
		return err
	}
	return nil
}

func UpgradeCRDs(namespace string, lhClient *lhclientset.Clientset) error {
	if err := doInstanceManagerUpgrade(namespace, lhClient); err != nil {
		logrus.Errorf(upgradeLogPrefix+"cannot finish InstanceManager upgrade: %v", err)
		return err
	}
	return nil
}

func doInstanceManagerUpgrade(namespace string, lhClient *lhclientset.Clientset) error {
	defer func() {
		logrus.Info(upgradeLogPrefix + "Upgrade instance managers completed")
	}()
	logrus.Info(upgradeLogPrefix + "Start the instance managers upgrade process")

	nodeMap := map[string]longhorn.Node{}
	nodeList, err := lhClient.LonghornV1beta1().Nodes(namespace).List(metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return errors.Wrapf(err, upgradeLogPrefix+"failed to list all nodes during the instance managers upgrade")
	}
	for _, node := range nodeList.Items {
		nodeMap[node.Name] = node
	}

	imList, err := lhClient.LonghornV1beta1().InstanceManagers(namespace).List(metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return errors.Wrapf(err, upgradeLogPrefix+"failed to list all existing instance managers during the instance managers upgrade")
	}
	for _, im := range imList.Items {
		if im.Spec.Image != "" {
			continue
		}
		im := &im
		if types.ValidateEngineImageChecksumName(im.Spec.EngineImage) {
			ei, err := lhClient.LonghornV1beta1().EngineImages(namespace).Get(im.Spec.EngineImage, metav1.GetOptions{})
			if err != nil {
				return errors.Wrapf(err, upgradeLogPrefix+"failed to find out the related engine image %v during the instance managers upgrade", im.Spec.EngineImage)
			}
			im.Spec.EngineImage = ei.Spec.Image
		}
		im.Spec.Image = im.Spec.EngineImage
		node, exist := nodeMap[im.Spec.NodeID]
		if !exist {
			return fmt.Errorf(upgradeLogPrefix+"cannot to find node %v for instance manager %v during the instance manager upgrade", im.Spec.NodeID, im.Name)
		}
		metadata, err := meta.Accessor(im)
		if err != nil {
			return err
		}
		metadata.SetOwnerReferences(datastore.GetOwnerReferencesForNode(&node))
		metadata.SetLabels(types.GetInstanceManagerLabels(im.Spec.NodeID, im.Spec.Image, im.Spec.Type))
		if im, err = lhClient.LonghornV1beta1().InstanceManagers(namespace).Update(im); err != nil {
			return errors.Wrapf(err, upgradeLogPrefix+"failed to update the spec for instance manager %v during the instance managers upgrade", im.Name)
		}

		im.Status.APIMinVersion = engineapi.IncompatibleInstanceManagerAPIVersion
		im.Status.APIVersion = engineapi.IncompatibleInstanceManagerAPIVersion
		if _, err = lhClient.LonghornV1beta1().InstanceManagers(namespace).UpdateStatus(im); err != nil {
			return errors.Wrapf(err, upgradeLogPrefix+"failed to update the version status for instance manager %v during the instance managers upgrade", im.Name)
		}
	}

	return nil
}