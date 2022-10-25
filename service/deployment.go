package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/wI2L/jsondiff"
	"initSkywalkingAgent/common"
	"initSkywalkingAgent/constants"
	adminssionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"sigs.k8s.io/yaml"
)

var (
	ignoredNamespaces = []string{
		metav1.NamespaceSystem,
		metav1.NamespaceDefault,
		metav1.NamespacePublic,
	}
)

func MutateDeploy(deploy *appsv1.Deployment) *adminssionv1.AdmissionResponse {
	SwBackEnd := os.Getenv("SW_AGENT_COLLECTOR_BACKEND_SERVICE")

	var log *bytes.Buffer

	resourceNS, resourceName, objectMeta := deploy.Namespace, deploy.Name, &deploy.ObjectMeta
	log.WriteString("\n----PreCheck----")
	if !common.RequiredMutate(ignoredNamespaces, objectMeta) {
		log.WriteString(fmt.Sprintf("\nSkip mutate for [#{resourceNS}/#{resourcename}]"))
		return &adminssionv1.AdmissionResponse{
			Allowed: true,
		}
	}
	log.WriteString("\n----EndCheck----")
	log.WriteString("\n----PreMutate----")
	newDp := deploy.DeepCopy()
	newPodSpec := &newDp.Spec.Template.Spec

	addVolume := corev1.Volume{
		Name: constants.VolumeName,
	}
	addFlag := true
	for _, v := range newPodSpec.Volumes {
		if v.Name == constants.VolumeName {
			addFlag = false
		}
	}
	if addFlag {
		newPodSpec.Volumes = append(newPodSpec.Volumes, addVolume)
		addVolumeMount := corev1.VolumeMount{
			Name:      constants.VolumeName,
			MountPath: constants.VolumeMountPath,
		}
		var addEnvs = []corev1.EnvVar{
			{
				Name:  "SW_AGENT_COLLECTOR_BACKEND_SERVICE",
				Value: SwBackEnd,
			},
			{
				Name:  "SW_AGENT_NAMESPACE",
				Value: resourceNS,
			},
			{
				Name:  "SW_AGENT_NAME",
				Value: resourceName,
			},
			{
				Name:  "JAVA_TOOL_OPTIONS",
				Value: constants.JavaToolOptions,
			},
		}
		containers := newPodSpec.Containers
		for i, _ := range containers {
			flag := true
			for _, v := range containers[i].VolumeMounts {
				if v.Name == constants.VolumeName {
					flag = false
				}
			}
			if flag {
				containers[i].VolumeMounts = append(containers[i].VolumeMounts, addVolumeMount)
			}
			for _, addEnv := range addEnvs {
				flag := true
				for _, v := range containers[i].Env {
					if v.Name == addEnv.Name {
						flag = false
					}
				}
				if flag {
					containers[i].Env = append(containers[i].Env, addEnv)
				}
			}
		}
		newPodSpec.Containers = containers
	}

	addInitContainerFlag := true
	for _, v := range newPodSpec.InitContainers {
		if v.Name == constants.InitContainerName {
			addInitContainerFlag = false
		}
	}
	if addInitContainerFlag {
		initContainer := corev1.Container{
			Name:    constants.InitContainerName,
			Image:   common.InitSwAgentImg,
			Command: []string{"/bin/sh"},
			Args:    []string{"-c", "cp -r /opt/skywalking-agent " + constants.VolumeMountPath},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      constants.VolumeName,
					MountPath: constants.VolumeMountPath,
				},
			},
		}
		initContainerReq := make(map[corev1.ResourceName]resource.Quantity)
		initContainerReq[corev1.ResourceCPU] = *resource.NewMilliQuantity(100, resource.DecimalSI)
		initContainerReq[corev1.ResourceMemory] = *resource.NewQuantity(100*1024*1024, resource.BinarySI)
		initContainer.Resources.Requests = initContainerReq
		newPodSpec.InitContainers = append(newPodSpec.InitContainers, initContainer)
	}
	log.WriteString("\n----EndMutate----")

	log.WriteString("\n----BeginMutateYaml----")
	bytes, err := json.Marshal(newPodSpec)
	if err == nil {
		yamlStr, err := yaml.JSONToYAML(bytes)
		if err == nil {
			log.WriteString("\n" + string(yamlStr))
		}
	}
	log.WriteString("\n----EndMutateYaml----")
	patch, err := jsondiff.Compare(deploy, newDp)
	if err != nil {
		log.WriteString(fmt.Sprintf("\nCompare patch error: #{err.Error()}"))
		return &adminssionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	patchBytes, err := json.MarshalIndent(patch, "", "	")
	if err != nil {
		log.WriteString(fmt.Sprintf("\nPatch error: #{err.Error()}"))
		return &adminssionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}
	log.WriteString(fmt.Sprintf("\nAdmissionResponse: patch=#{string(patchBytes)}\n"))
	return &adminssionv1.AdmissionResponse{
		Allowed: true,
		Patch:   patchBytes,
		PatchType: func() *adminssionv1.PatchType {
			pt := adminssionv1.PatchTypeJSONPatch
			return &pt
		}(),
	}
}
