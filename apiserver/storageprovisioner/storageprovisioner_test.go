// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package storageprovisioner_test

import (
	"sort"

	"github.com/juju/errors"
	"github.com/juju/names"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/apiserver/storageprovisioner"
	apiservertesting "github.com/juju/juju/apiserver/testing"
	"github.com/juju/juju/instance"
	jujutesting "github.com/juju/juju/juju/testing"
	"github.com/juju/juju/state"
	statetesting "github.com/juju/juju/state/testing"
	"github.com/juju/juju/storage"
	"github.com/juju/juju/storage/provider/dummy"
	"github.com/juju/juju/storage/provider/registry"
	"github.com/juju/juju/testing/factory"
)

var _ = gc.Suite(&provisionerSuite{})

type provisionerSuite struct {
	// TODO(wallyworld) remove JujuConnSuite
	jujutesting.JujuConnSuite

	factory    *factory.Factory
	resources  *common.Resources
	authorizer *apiservertesting.FakeAuthorizer
	api        *storageprovisioner.StorageProvisionerAPI
}

func (s *provisionerSuite) SetUpSuite(c *gc.C) {
	s.JujuConnSuite.SetUpSuite(c)

	registry.RegisterProvider("environscoped", &dummy.StorageProvider{
		StorageScope: storage.ScopeEnviron,
	})
	registry.RegisterProvider("machinescoped", &dummy.StorageProvider{
		StorageScope: storage.ScopeMachine,
	})
	registry.RegisterEnvironStorageProviders(
		"dummy", "environscoped", "machinescoped",
	)
	s.AddSuiteCleanup(func(c *gc.C) {
		registry.RegisterProvider("environscoped", nil)
		registry.RegisterProvider("machinescoped", nil)
	})
}

func (s *provisionerSuite) SetUpTest(c *gc.C) {
	s.JujuConnSuite.SetUpTest(c)
	s.factory = factory.NewFactory(s.State)
	s.resources = common.NewResources()
	// Create the resource registry separately to track invocations to
	// Register.
	s.resources = common.NewResources()
	s.AddCleanup(func(_ *gc.C) { s.resources.StopAll() })

	var err error
	s.authorizer = &apiservertesting.FakeAuthorizer{
		Tag:            names.NewMachineTag("0"),
		EnvironManager: true,
	}
	s.api, err = storageprovisioner.NewStorageProvisionerAPI(s.State, s.resources, s.authorizer)
	c.Assert(err, jc.ErrorIsNil)
}

func (s *provisionerSuite) TestNewStorageProvisionerAPINonMachine(c *gc.C) {
	tag := names.NewUnitTag("mysql/0")
	authorizer := &apiservertesting.FakeAuthorizer{Tag: tag}
	_, err := storageprovisioner.NewStorageProvisionerAPI(s.State, common.NewResources(), authorizer)
	c.Assert(err, gc.ErrorMatches, "permission denied")
}

func (s *provisionerSuite) setupVolumes(c *gc.C) {
	s.factory.MakeMachine(c, &factory.MachineParams{
		InstanceId: instance.Id("inst-id"),
		Volumes: []state.MachineVolumeParams{
			{Volume: state.VolumeParams{Pool: "machinescoped", Size: 1024}},
			{Volume: state.VolumeParams{Pool: "environscoped", Size: 2048}},
			{Volume: state.VolumeParams{Pool: "environscoped", Size: 4096}},
		},
	})
	// Only provision the first and third volumes.
	err := s.State.SetVolumeInfo(names.NewVolumeTag("0/0"), state.VolumeInfo{
		Serial:   "123",
		VolumeId: "abc",
		Size:     1024,
	})
	c.Assert(err, jc.ErrorIsNil)
	err = s.State.SetVolumeInfo(names.NewVolumeTag("2"), state.VolumeInfo{
		Serial:   "456",
		VolumeId: "def",
		Size:     4096,
	})
	c.Assert(err, jc.ErrorIsNil)

	// Make a machine without storage for tests to use.
	s.factory.MakeMachine(c, nil)

	// Make an unprovisioned machine with storage for tests to use.
	// TODO(axw) extend testing/factory to allow creating unprovisioned
	// machines.
	_, err = s.State.AddOneMachine(state.MachineTemplate{
		Series: "quantal",
		Jobs:   []state.MachineJob{state.JobHostUnits},
		Volumes: []state.MachineVolumeParams{
			{Volume: state.VolumeParams{Pool: "environscoped", Size: 2048}},
		},
	})
	c.Assert(err, jc.ErrorIsNil)
}

func (s *provisionerSuite) TestVolumesMachine(c *gc.C) {
	s.setupVolumes(c)
	s.authorizer.EnvironManager = false

	results, err := s.api.Volumes(params.Entities{
		Entities: []params.Entity{{"volume-0-0"}, {"volume-1"}, {"volume-42"}},
	})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(results, gc.DeepEquals, params.VolumeResults{
		Results: []params.VolumeResult{
			{Result: params.Volume{VolumeTag: "volume-0-0", VolumeId: "abc", Serial: "123", Size: 1024}},
			{Error: &params.Error{"permission denied", "unauthorized access"}},
			{Error: &params.Error{"permission denied", "unauthorized access"}},
		},
	})
}

func (s *provisionerSuite) TestVolumesEnviron(c *gc.C) {
	s.setupVolumes(c)
	s.authorizer.Tag = names.NewMachineTag("2") // neither 0 nor 1

	results, err := s.api.Volumes(params.Entities{
		Entities: []params.Entity{
			{"volume-0-0"},
			{"volume-1"},
			{"volume-2"},
			{"volume-42"},
		},
	})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(results, gc.DeepEquals, params.VolumeResults{
		Results: []params.VolumeResult{
			{Error: &params.Error{"permission denied", "unauthorized access"}},
			{Error: common.ServerError(errors.NotProvisionedf(`volume "1"`))},
			{Result: params.Volume{VolumeTag: "volume-2", VolumeId: "def", Serial: "456", Size: 4096}},
			{Error: &params.Error{"permission denied", "unauthorized access"}},
		},
	})
}

func (s *provisionerSuite) TestVolumesEmptyArgs(c *gc.C) {
	results, err := s.api.Volumes(params.Entities{})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(results.Results, gc.HasLen, 0)
}

func (s *provisionerSuite) TestVolumeAttachments(c *gc.C) {
	s.setupVolumes(c)
	s.authorizer.EnvironManager = false

	err := s.State.SetVolumeAttachmentInfo(
		names.NewMachineTag("0"),
		names.NewVolumeTag("0/0"),
		state.VolumeAttachmentInfo{DeviceName: "xvdf1"},
	)
	c.Assert(err, jc.ErrorIsNil)

	results, err := s.api.VolumeAttachments(params.MachineStorageIds{
		Ids: []params.MachineStorageId{{
			MachineTag:    "machine-0",
			AttachmentTag: "volume-0-0",
		}, {
			MachineTag:    "machine-0",
			AttachmentTag: "volume-2", // volume attachment not provisioned
		}, {
			MachineTag:    "machine-0",
			AttachmentTag: "volume-1", // volume not provisioned
		}, {
			MachineTag:    "machine-0",
			AttachmentTag: "volume-42",
		}},
	})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(results, jc.DeepEquals, params.VolumeAttachmentResults{
		Results: []params.VolumeAttachmentResult{
			{Result: params.VolumeAttachment{
				VolumeTag: "volume-0-0", MachineTag: "machine-0", DeviceName: "xvdf1",
			}},
			{Error: &params.Error{
				Code:    params.CodeNotProvisioned,
				Message: `volume attachment "2" on "0" not provisioned`,
			}},
			{Error: &params.Error{
				Code:    params.CodeNotProvisioned,
				Message: `volume attachment "1" on "0" not provisioned`,
			}},
			{Error: &params.Error{"permission denied", "unauthorized access"}},
		},
	})
}

func (s *provisionerSuite) TestVolumeParams(c *gc.C) {
	s.setupVolumes(c)
	results, err := s.api.VolumeParams(params.Entities{
		Entities: []params.Entity{{"volume-0-0"}, {"volume-1"}, {"volume-42"}},
	})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(results, gc.DeepEquals, params.VolumeParamsResults{
		Results: []params.VolumeParamsResult{
			{Error: &params.Error{`volume "0/0" is already provisioned`, ""}},
			{Result: params.VolumeParams{
				VolumeTag: "volume-1",
				Size:      2048,
				Provider:  "environscoped",
				Attachment: &params.VolumeAttachmentParams{
					MachineTag: "machine-0",
				},
			}},
			{Error: &params.Error{"permission denied", "unauthorized access"}},
		},
	})
}

func (s *provisionerSuite) TestVolumeParamsEmptyArgs(c *gc.C) {
	results, err := s.api.VolumeParams(params.Entities{})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(results.Results, gc.HasLen, 0)
}

func (s *provisionerSuite) TestVolumeAttachmentParams(c *gc.C) {
	s.setupVolumes(c)
	s.authorizer.EnvironManager = true

	results, err := s.api.VolumeAttachmentParams(params.MachineStorageIds{
		Ids: []params.MachineStorageId{{
			MachineTag:    "machine-0",
			AttachmentTag: "volume-0-0",
		}, {
			MachineTag:    "machine-0",
			AttachmentTag: "volume-1",
		}, {
			MachineTag:    "machine-2",
			AttachmentTag: "volume-3",
		}, {
			MachineTag:    "machine-0",
			AttachmentTag: "volume-42",
		}},
	})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(results, jc.DeepEquals, params.VolumeAttachmentParamsResults{
		Results: []params.VolumeAttachmentParamsResult{
			{Result: params.VolumeAttachmentParams{
				MachineTag: "machine-0",
				VolumeTag:  "volume-0-0",
				InstanceId: "inst-id",
				VolumeId:   "abc",
				Provider:   "machinescoped",
			}},
			{Error: &params.Error{
				Code:    params.CodeNotProvisioned,
				Message: `volume "1" not provisioned`,
			}},
			{Error: &params.Error{
				Code:    params.CodeNotProvisioned,
				Message: `machine 2 not provisioned`,
			}},
			{Error: &params.Error{"permission denied", "unauthorized access"}},
		},
	})
}

func (s *provisionerSuite) TestWatchVolumes(c *gc.C) {
	s.setupVolumes(c)
	s.factory.MakeMachine(c, nil)
	c.Assert(s.resources.Count(), gc.Equals, 0)

	args := params.Entities{Entities: []params.Entity{
		{"machine-0"},
		{s.State.EnvironTag().String()},
		{"environ-adb650da-b77b-4ee8-9cbb-d57a9a592847"},
		{"machine-1"},
		{"machine-42"}},
	}
	result, err := s.api.WatchVolumes(args)
	c.Assert(err, jc.ErrorIsNil)
	sort.Strings(result.Results[1].Changes)
	c.Assert(result, gc.DeepEquals, params.StringsWatchResults{
		Results: []params.StringsWatchResult{
			{StringsWatcherId: "1", Changes: []string{"0/0"}},
			{StringsWatcherId: "2", Changes: []string{"1", "2"}},
			{Error: apiservertesting.ErrUnauthorized},
			{Error: apiservertesting.ErrUnauthorized},
			{Error: apiservertesting.ErrUnauthorized},
		},
	})

	// Verify the resources were registered and stop them when done.
	c.Assert(s.resources.Count(), gc.Equals, 2)
	v0Watcher := s.resources.Get("1")
	defer statetesting.AssertStop(c, v0Watcher)
	v1Watcher := s.resources.Get("2")
	defer statetesting.AssertStop(c, v1Watcher)

	// Check that the Watch has consumed the initial events ("returned" in
	// the Watch call)
	wc := statetesting.NewStringsWatcherC(c, s.State, v0Watcher.(state.StringsWatcher))
	wc.AssertNoChange()
	wc = statetesting.NewStringsWatcherC(c, s.State, v1Watcher.(state.StringsWatcher))
	wc.AssertNoChange()
}

func (s *provisionerSuite) TestWatchVolumeAttachments(c *gc.C) {
	s.setupVolumes(c)
	s.factory.MakeMachine(c, nil)
	c.Assert(s.resources.Count(), gc.Equals, 0)

	args := params.Entities{Entities: []params.Entity{
		{"machine-0"},
		{s.State.EnvironTag().String()},
		{"environ-adb650da-b77b-4ee8-9cbb-d57a9a592847"},
		{"machine-1"},
		{"machine-42"}},
	}
	result, err := s.api.WatchVolumeAttachments(args)
	c.Assert(err, jc.ErrorIsNil)
	sort.Sort(byMachineAndEntity(result.Results[0].Changes))
	sort.Sort(byMachineAndEntity(result.Results[1].Changes))
	c.Assert(result, jc.DeepEquals, params.MachineStorageIdsWatchResults{
		Results: []params.MachineStorageIdsWatchResult{
			{
				MachineStorageIdsWatcherId: "1",
				Changes: []params.MachineStorageId{{
					MachineTag:    "machine-0",
					AttachmentTag: "volume-0-0",
				}, {
					MachineTag:    "machine-0",
					AttachmentTag: "volume-1",
				}, {
					MachineTag:    "machine-0",
					AttachmentTag: "volume-2",
				}},
			},
			{
				MachineStorageIdsWatcherId: "2",
				Changes: []params.MachineStorageId{{
					MachineTag:    "machine-0",
					AttachmentTag: "volume-1",
				}, {
					MachineTag:    "machine-0",
					AttachmentTag: "volume-2",
				}},
			},
			{Error: apiservertesting.ErrUnauthorized},
			{Error: apiservertesting.ErrUnauthorized},
			{Error: apiservertesting.ErrUnauthorized},
		},
	})

	// Verify the resources were registered and stop them when done.
	c.Assert(s.resources.Count(), gc.Equals, 2)
	v0Watcher := s.resources.Get("1")
	defer statetesting.AssertStop(c, v0Watcher)
	v1Watcher := s.resources.Get("2")
	defer statetesting.AssertStop(c, v1Watcher)

	// Check that the Watch has consumed the initial events ("returned" in
	// the Watch call)
	wc := statetesting.NewStringsWatcherC(c, s.State, v0Watcher.(state.StringsWatcher))
	wc.AssertNoChange()
	wc = statetesting.NewStringsWatcherC(c, s.State, v1Watcher.(state.StringsWatcher))
	wc.AssertNoChange()
}

func (s *provisionerSuite) TestLife(c *gc.C) {
	s.setupVolumes(c)
	args := params.Entities{Entities: []params.Entity{{"volume-0-0"}, {"volume-1"}, {"volume-42"}}}
	result, err := s.api.Life(args)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(result, gc.DeepEquals, params.LifeResults{
		Results: []params.LifeResult{
			{Life: params.Alive},
			{Life: params.Alive},
			{Error: common.ServerError(errors.NotFoundf(`volume "42"`))},
		},
	})
}

func (s *provisionerSuite) TestEnsureDead(c *gc.C) {
	s.setupVolumes(c)
	args := params.Entities{Entities: []params.Entity{{"volume-0-0"}, {"volume-1"}, {"volume-42"}}}
	result, err := s.api.EnsureDead(args)
	c.Assert(err, jc.ErrorIsNil)
	// TODO(wallyworld) - this test will be updated when EnsureDead is supported
	c.Assert(result, gc.DeepEquals, params.ErrorResults{
		Results: []params.ErrorResult{
			{Error: common.ServerError(common.NotSupportedError(names.NewVolumeTag("0/0"), "ensuring death"))},
			{Error: common.ServerError(common.NotSupportedError(names.NewVolumeTag("1"), "ensuring death"))},
			{Error: common.ServerError(errors.NotFoundf(`volume "42"`))},
		},
	})
}

type byMachineAndEntity []params.MachineStorageId

func (b byMachineAndEntity) Len() int {
	return len(b)
}

func (b byMachineAndEntity) Less(i, j int) bool {
	if b[i].MachineTag == b[j].MachineTag {
		return b[i].AttachmentTag < b[j].AttachmentTag
	}
	return b[i].MachineTag < b[j].MachineTag
}

func (b byMachineAndEntity) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}
