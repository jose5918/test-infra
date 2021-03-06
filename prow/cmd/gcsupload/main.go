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

// gcsupload uploads the files and folders specified
// to GCS using the Prow-defined job configuration
package main

import (
	"flag"
	"os"

	"github.com/sirupsen/logrus"

	"k8s.io/test-infra/prow/logrusutil"
	"k8s.io/test-infra/prow/pod-utils/gcs"
)

func main() {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	o := gcs.BindOptions(fs)
	fs.Parse(os.Args[1:])
	o.Complete(fs.Args())

	if err := o.Validate(); err != nil {
		logrus.Fatalf("Invalid options: %v", err)
	}

	logrus.SetFormatter(
		logrusutil.NewDefaultFieldsFormatter(nil, logrus.Fields{"component": "gcsupload"}),
	)

	if err := o.Run(map[string]gcs.UploadFunc{}); err != nil {
		logrus.WithError(err).Fatal("Failed to upload to GCS")
	}
}
