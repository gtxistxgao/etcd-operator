// Copyright 2017 The etcd-operator Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package backup

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/coreos/etcd-operator/pkg/backup/abs"
)

const integrationTestEnvVar = "RUN_INTEGRATION_TEST"

var (
	accountName    = storage.StorageEmulatorAccountName
	accountKey     = storage.StorageEmulatorAccountKey
	DefaultBaseURL = "http://127.0.0.1:10000"
	prefix         = "testprefix"
	blobContents   = "ignore"

	absIntegrationTestNotSet = fmt.Sprintf("skipping ABS integration test due to %s not set", integrationTestEnvVar)
)

func generateRandomContainerName() (string, error) {
	n := 5
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	randContainerName := fmt.Sprintf("testcontainer-%X", b)

	return randContainerName, nil
}

func TestABSBackendContainerDoesNotExist(t *testing.T) {
	if os.Getenv(integrationTestEnvVar) != "true" {
		t.Skip(absIntegrationTestNotSet)
	}

	container, err := generateRandomContainerName()
	if err != nil {
		t.Fatal(err)
	}

	storageClient, err := storage.NewClient(accountName, accountKey, DefaultBaseURL, "", false)
	if err != nil {
		t.Fatal(err)
	}

	_, err = abs.NewFromClient(container, prefix, &storageClient)
	if err == nil {
		t.Fatal(err)
	}
	if err.Error() != fmt.Sprintf("container %s does not exist", container) {
		t.Fatal(err)
	}
}

func TestABSBackendGetLatestWithActualData(t *testing.T) {

	input := `v1/5b02a9edd757c500016b6ea2/etcd-5b02a9edd757c500016b6ea2/3.1.19_0000000000ed0981_etcd.backup	BlockBlob	Cool	1904672	application/octet-stream	2018-10-04T15:52:09+00:00
v1/5b02a9edd757c500016b6ea2/etcd-5b02a9edd757c500016b6ea2/3.1.19_0000000000ed1e1c_etcd.backup	BlockBlob	Cool	2244640	application/octet-stream	2018-10-04T16:54:10+00:00
v1/5b02a9edd757c500016b6ea2/etcd-5b02a9edd757c500016b6ea2/3.1.8_0000000000e95028_etcd.backup	BlockBlob	Cool	1523744	application/octet-stream	2018-10-02T12:09:08+00:00
v1/5b02a9edd757c500016b6ea2/etcd-5b02a9edd757c500016b6ea2/3.1.8_0000000000e959d6_etcd.backup	BlockBlob	Cool	1863712	application/octet-stream	2018-10-02T12:41:09+00:00
v1/5b02a9edd757c500016b6ea2/etcd-5b02a9edd757c500016b6ea2/3.1.8_0000000000e96381_etcd.backup	BlockBlob	Cool	1380384	application/octet-stream	2018-10-02T13:13:11+00:00
v1/5b02a9edd757c500016b6ea2/etcd-5b02a9edd757c500016b6ea2/3.1.8_0000000000e96d2d_etcd.backup	BlockBlob	Cool	1716256	application/octet-stream	2018-10-02T13:45:12+00:00
v1/5b02a9edd757c500016b6ea2/etcd-5b02a9edd757c500016b6ea2/3.1.8_0000000000e976e0_etcd.backup	BlockBlob	Cool	2068512	application/octet-stream	2018-10-02T14:17:18+00:00`

	lines := strings.Split(input, "\n")
	var blobs []storage.Blob
	for _, line := range lines {
		parts := strings.Split(line, "\t")
		name := parts[0]
		date, err := time.Parse("2006-01-02T15:04:05+00:00", parts[5])
		if err != nil {
			t.Errorf("Failed to parse date: %v", err)
		}
		blobs = append(blobs, storage.Blob{
			Name: name,
			Properties: storage.BlobProperties{
				LastModified: storage.TimeRFC1123(date),
			},
		})
	}

	backupName := getLatestBackupNameByDate(blobs)
	if "v1/5b02a9edd757c500016b6ea2/etcd-5b02a9edd757c500016b6ea2/3.1.19_0000000000ed1e1c_etcd.backup" != backupName {
		t.Errorf("Found the wrong blob: %s", backupName)
	}
}

func TestABSBackendGetLatest(t *testing.T) {
	if os.Getenv(integrationTestEnvVar) != "true" {
		t.Skip(absIntegrationTestNotSet)
	}

	container, err := generateRandomContainerName()
	if err != nil {
		t.Fatal(err)
	}

	storageClient, err := storage.NewClient(accountName, accountKey, DefaultBaseURL, "", false)
	if err != nil {
		t.Fatal(err)
	}
	blobServiceClient := storageClient.GetBlobService()

	// Create container
	cnt := blobServiceClient.GetContainerReference(container)
	options := storage.CreateContainerOptions{
		Access: storage.ContainerAccessTypePrivate,
	}
	_, err = cnt.CreateIfNotExists(&options)
	if err != nil {
		t.Fatal(err, "Create container failed")
	}
	defer func() {
		// Delete container
		opts := storage.DeleteContainerOptions{}
		if err := cnt.Delete(&opts); err != nil {
			t.Fatal(err)
		}
	}()

	abs, err := abs.NewFromClient(container, prefix, &storageClient)
	if err != nil {
		t.Fatal(err)
	}
	ab := &absBackend{ABS: abs}

	if _, err := ab.save("3.1.0", 1, bytes.NewBuffer([]byte(blobContents))); err != nil {
		t.Fatal(err)
	}
	if _, err := ab.save("3.1.1", 2, bytes.NewBuffer([]byte(blobContents))); err != nil {
		t.Fatal(err)
	}

	// test getLatest
	name, err := ab.getLatest()
	if err != nil {
		t.Fatal(err)
	}

	expected := makeBackupName("3.1.1", 2)
	if name != expected {
		t.Errorf("lastest name = %s, want %s", name, expected)
	}

	// test total
	totalBackups, err := ab.total()
	if err != nil {
		t.Fatal(err)
	}
	if totalBackups != 2 {
		t.Errorf("total backups = %v, want %v", totalBackups, 2)
	}

	// test open
	rc, err := ab.open(name)
	if err != nil {
		t.Fatal(err)
	}

	b, err := ioutil.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()

	if string(b) != blobContents {
		t.Errorf("content = %s, want %s", string(b), blobContents)
	}
}

func TestABSBackendPurge(t *testing.T) {
	if os.Getenv(integrationTestEnvVar) != "true" {
		t.Skip(absIntegrationTestNotSet)
	}

	container, err := generateRandomContainerName()
	if err != nil {
		t.Fatal(err)
	}

	storageClient, err := storage.NewClient(accountName, accountKey, DefaultBaseURL, "", false)
	if err != nil {
		t.Fatal(err)
	}
	blobServiceClient := storageClient.GetBlobService()

	// Create container
	cnt := blobServiceClient.GetContainerReference(container)
	options := storage.CreateContainerOptions{
		Access: storage.ContainerAccessTypePrivate,
	}
	_, err = cnt.CreateIfNotExists(&options)
	if err != nil {
		t.Fatal(err, "Create container failed")
	}
	defer func() {
		// Delete container
		opts := storage.DeleteContainerOptions{}
		if err := cnt.Delete(&opts); err != nil {
			t.Fatal(err)
		}
	}()

	abs, err := abs.NewFromClient(container, prefix, &storageClient)
	if err != nil {
		t.Fatal(err)
	}
	ab := &absBackend{ABS: abs}

	if _, err := ab.save("3.1.0", 1, bytes.NewBuffer([]byte(blobContents))); err != nil {
		t.Fatal(err)
	}
	if _, err := ab.save("3.1.0", 2, bytes.NewBuffer([]byte(blobContents))); err != nil {
		t.Fatal(err)
	}
	if err := ab.purge(1); err != nil {
		t.Fatal(err)
	}
	names, err := abs.List()
	if err != nil {
		t.Fatal(err)
	}
	leftFiles := []string{makeBackupName("3.1.0", 2)}
	if !reflect.DeepEqual(leftFiles, names) {
		t.Errorf("left files after purge, want=%v, get=%v", leftFiles, names)
	}
	if err := abs.Delete(makeBackupName("3.1.0", 2)); err != nil {
		t.Fatal(err)
	}
}
