/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package pjutil contains helpers for working with ProwJobs.
package pjutil

import (
	"fmt"

	uuid "github.com/satori/go.uuid"
	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/test-infra/prow/config"
	"k8s.io/test-infra/prow/github"
	"k8s.io/test-infra/prow/kube"
)

// NewProwJob initializes a ProwJob out of a ProwJobSpec.
func NewProwJob(spec kube.ProwJobSpec, labels map[string]string) kube.ProwJob {
	return kube.ProwJob{
		APIVersion: "prow.k8s.io/v1",
		Kind:       "ProwJob",
		ObjectMeta: metav1.ObjectMeta{
			Name:   uuid.NewV1().String(),
			Labels: labels,
		},
		Spec: spec,
		Status: kube.ProwJobStatus{
			StartTime: metav1.Now(),
			State:     kube.TriggeredState,
		},
	}
}

// PresubmitSpec initializes a ProwJobSpec for a given presubmit job.
func PresubmitSpec(p config.Presubmit, refs kube.Refs) kube.ProwJobSpec {
	pjs := kube.ProwJobSpec{
		Type: kube.PresubmitJob,
		Job:  p.Name,
		Refs: refs,

		Report:         !p.SkipReport,
		Context:        p.Context,
		RerunCommand:   p.RerunCommand,
		MaxConcurrency: p.MaxConcurrency,
	}
	pjs.Agent = kube.ProwJobAgent(p.Agent)
	if pjs.Agent == kube.KubernetesAgent {
		pjs.PodSpec = *p.Spec
		pjs.Cluster = p.Cluster
		if pjs.Cluster == "" {
			pjs.Cluster = kube.DefaultClusterAlias
		}
	}
	for _, nextP := range p.RunAfterSuccess {
		pjs.RunAfterSuccess = append(pjs.RunAfterSuccess, PresubmitSpec(nextP, refs))
	}
	return pjs
}

// PostsubmitSpec initializes a ProwJobSpec for a given postsubmit job.
func PostsubmitSpec(p config.Postsubmit, refs kube.Refs) kube.ProwJobSpec {
	pjs := kube.ProwJobSpec{
		Type:           kube.PostsubmitJob,
		Job:            p.Name,
		Refs:           refs,
		MaxConcurrency: p.MaxConcurrency,
	}
	pjs.Agent = kube.ProwJobAgent(p.Agent)
	if pjs.Agent == kube.KubernetesAgent {
		pjs.PodSpec = *p.Spec
		pjs.Cluster = p.Cluster
		if pjs.Cluster == "" {
			pjs.Cluster = kube.DefaultClusterAlias
		}
	}
	for _, nextP := range p.RunAfterSuccess {
		pjs.RunAfterSuccess = append(pjs.RunAfterSuccess, PostsubmitSpec(nextP, refs))
	}
	return pjs
}

// PeriodicSpec initializes a ProwJobSpec for a given periodic job.
func PeriodicSpec(p config.Periodic) kube.ProwJobSpec {
	pjs := kube.ProwJobSpec{
		Type: kube.PeriodicJob,
		Job:  p.Name,
	}
	pjs.Agent = kube.ProwJobAgent(p.Agent)
	if pjs.Agent == kube.KubernetesAgent {
		pjs.PodSpec = *p.Spec
		pjs.Cluster = p.Cluster
		if pjs.Cluster == "" {
			pjs.Cluster = kube.DefaultClusterAlias
		}
	}
	for _, nextP := range p.RunAfterSuccess {
		pjs.RunAfterSuccess = append(pjs.RunAfterSuccess, PeriodicSpec(nextP))
	}
	return pjs
}

// BatchSpec initializes a ProwJobSpec for a given batch job and ref spec.
func BatchSpec(p config.Presubmit, refs kube.Refs) kube.ProwJobSpec {
	pjs := kube.ProwJobSpec{
		Type:    kube.BatchJob,
		Job:     p.Name,
		Refs:    refs,
		Context: p.Context, // The Submit Queue's getCompleteBatches needs this.
	}
	pjs.Agent = kube.ProwJobAgent(p.Agent)
	if pjs.Agent == kube.KubernetesAgent {
		pjs.PodSpec = *p.Spec
		pjs.Cluster = p.Cluster
		if pjs.Cluster == "" {
			pjs.Cluster = kube.DefaultClusterAlias
		}
	}
	for _, nextP := range p.RunAfterSuccess {
		pjs.RunAfterSuccess = append(pjs.RunAfterSuccess, BatchSpec(nextP, refs))
	}
	return pjs
}

// ProwJobToPod converts a ProwJob to a Pod that will run the tests.
func ProwJobToPod(pj kube.ProwJob, buildID string) (*v1.Pod, error) {
	env, err := EnvForSpec(NewJobSpec(pj.Spec, buildID, pj.Name))
	if err != nil {
		return nil, err
	}

	spec := pj.Spec.PodSpec
	spec.RestartPolicy = "Never"

	// Set environment variables in each container in the pod spec. We don't
	// want to update the spec in place, since that will update the ProwJob
	// spec. Instead, create a copy.
	spec.InitContainers = []v1.Container{}
	for i := range pj.Spec.PodSpec.InitContainers {
		spec.InitContainers = append(spec.InitContainers, pj.Spec.PodSpec.InitContainers[i])
		if spec.InitContainers[i].Name == "" {
			spec.InitContainers[i].Name = fmt.Sprintf("%s-%d", pj.ObjectMeta.Name, i)
		}
		spec.InitContainers[i].Env = append(spec.InitContainers[i].Env, kubeEnv(env)...)
	}
	spec.Containers = []v1.Container{}
	for i := range pj.Spec.PodSpec.Containers {
		spec.Containers = append(spec.Containers, pj.Spec.PodSpec.Containers[i])
		if spec.Containers[i].Name == "" {
			spec.Containers[i].Name = fmt.Sprintf("%s-%d", pj.ObjectMeta.Name, i)
		}
		spec.Containers[i].Env = append(spec.Containers[i].Env, kubeEnv(env)...)
	}
	podLabels := make(map[string]string)
	for k, v := range pj.ObjectMeta.Labels {
		podLabels[k] = v
	}
	podLabels[kube.CreatedByProw] = "true"
	podLabels[kube.ProwJobTypeLabel] = string(pj.Spec.Type)
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   pj.ObjectMeta.Name,
			Labels: podLabels,
			Annotations: map[string]string{
				kube.ProwJobAnnotation: pj.Spec.Job,
			},
		},
		Spec: spec,
	}, nil
}

// kubeEnv transforms a mapping of environment variables
// into their serialized form for a PodSpec
func kubeEnv(environment map[string]string) []v1.EnvVar {
	var kubeEnvironment []v1.EnvVar
	for key, value := range environment {
		kubeEnvironment = append(kubeEnvironment, v1.EnvVar{
			Name:  key,
			Value: value,
		})
	}

	return kubeEnvironment
}

// PartitionActive separates the provided prowjobs into pending and triggered
// and returns them inside channels so that they can be consumed in parallel
// by different goroutines. Complete prowjobs are filtered out. Controller
// loops need to handle pending jobs first so they can conform to maximum
// concurrency requirements that different jobs may have.
func PartitionActive(pjs []kube.ProwJob) (pending, triggered chan kube.ProwJob) {
	// Size channels correctly.
	pendingCount, triggeredCount := 0, 0
	for _, pj := range pjs {
		switch pj.Status.State {
		case kube.PendingState:
			pendingCount++
		case kube.TriggeredState:
			triggeredCount++
		}
	}
	pending = make(chan kube.ProwJob, pendingCount)
	triggered = make(chan kube.ProwJob, triggeredCount)

	// Partition the jobs into the two separate channels.
	for _, pj := range pjs {
		switch pj.Status.State {
		case kube.PendingState:
			pending <- pj
		case kube.TriggeredState:
			triggered <- pj
		}
	}
	close(pending)
	close(triggered)
	return pending, triggered
}

// GetLatestProwJobs filters through the provided prowjobs and returns
// a map of jobType jobs to their latest prowjobs.
func GetLatestProwJobs(pjs []kube.ProwJob, jobType kube.ProwJobType) map[string]kube.ProwJob {
	latestJobs := make(map[string]kube.ProwJob)
	for _, j := range pjs {
		if j.Spec.Type != jobType {
			continue
		}
		name := j.Spec.Job
		if j.Status.StartTime.After(latestJobs[name].Status.StartTime.Time) {
			latestJobs[name] = j
		}
	}
	return latestJobs
}

// ProwJobFields extracts logrus fields from a prowjob useful for logging.
func ProwJobFields(pj *kube.ProwJob) logrus.Fields {
	fields := make(logrus.Fields)
	fields["name"] = pj.ObjectMeta.Name
	fields["job"] = pj.Spec.Job
	fields["type"] = pj.Spec.Type
	if len(pj.ObjectMeta.Labels[github.EventGUID]) > 0 {
		fields[github.EventGUID] = pj.ObjectMeta.Labels[github.EventGUID]
	}
	if len(pj.Spec.Refs.Pulls) == 1 {
		fields[github.PrLogField] = pj.Spec.Refs.Pulls[0].Number
		fields[github.RepoLogField] = pj.Spec.Refs.Repo
		fields[github.OrgLogField] = pj.Spec.Refs.Org
	}
	return fields
}
