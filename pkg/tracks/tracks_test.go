package tracks_test

import (
	"flag"
	"fmt"
	"github.com/golang/mock/gomock"
	"github.com/sirupsen/logrus"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.optum.com/healthcarecloud/terrascale/mocks"
	"github.optum.com/healthcarecloud/terrascale/pkg/config"
	"github.optum.com/healthcarecloud/terrascale/pkg/steps"
	"github.optum.com/healthcarecloud/terrascale/pkg/tracks"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var DefaultStubAccountID string
var StubVersion string

var fs afero.Fs
var logger *logrus.Entry
var stepperFactory steps.StepperFactory
var sut tracks.Tracker

var stubTrackCount int
var stubStepCount int
var stubStepTestsCount int

var stubStepWithTests steps.Step
var stubTracks map[string]tracks.Track
var stubTrackNameA string
var stubTrackNameB string

func TestMain(m *testing.M) {
	// arrange

	fs = afero.NewMemMapFs()
	logs := logrus.New()
	logs.SetReportCaller(true)
	logger = logs.WithField("environment", "unittest")

	DefaultStubAccountID = "1"
	StubVersion = "v0.0.5"

	stubTrackCount = 2
	stubStepCount = 5
	stubStepTestsCount = 0

	tracks.DestroyTrack = tracks.ExecuteDestroyTrack
	tracks.DeployTrack = tracks.ExecuteDeployTrack
	tracks.DeployTrackRegion = tracks.ExecuteDeployTrackRegion
	tracks.DestroyTrackRegion = tracks.ExecuteDestroyTrackRegion
	tracks.ExecuteStep = tracks.ExecuteStepImpl

	sut = tracks.DirectoryBasedTracker{
		Fs:  fs,
		Log: logger,
	}

	stubStepWithTests = steps.Step{
		Name:                   "a11",
		TestsExist:             true,
		ProgressionLevel:       1,
		TrackName:              "track",
		RegionalResourcesExist: true,
	}

	stubTrackNameA = "track-a"
	stubTrackNameB = "track-b"

	stubTracks = map[string]tracks.Track{
		stubTrackNameA: {
			Name:                  stubTrackNameA,
			StepProgressionsCount: 2,
			OrderedSteps: map[int][]steps.Step{
				1: {
					stubStepWithTests,
					{
						Name:             "a12",
						TestsExist:       false,
						ProgressionLevel: 1,
					},
				},
				2: {
					{
						Name:             "a21",
						TestsExist:       false,
						ProgressionLevel: 2,
					},
				},
			},
		},
		stubTrackNameB: {
			Name:                  stubTrackNameB,
			StepProgressionsCount: 1,
			OrderedSteps: map[int][]steps.Step{
				1: {
					{
						Name:             "b11",
						TestsExist:       false,
						ProgressionLevel: 1,
					},
					{
						Name:             "b12",
						TestsExist:       false,
						ProgressionLevel: 1,
					},
				},
			},
		},
	}

	for key, track := range stubTracks {
		track.Dir = fmt.Sprintf("tracks/%s", track.Name)

		for progression, stubSteps := range track.OrderedSteps {
			for _, stubStep := range stubSteps {

				stepDir := fmt.Sprintf("%s/step%v_%v", track.Dir, progression, stubStep.Name)
				fs.MkdirAll(stepDir, 0755)

				track.StepsCount++

				if stubStep.TestsExist {
					stubStepTestsCount++
					track.StepsWithTestsCount++
					fs.MkdirAll(fmt.Sprintf("%s/tests", stepDir), 0755)
					_ = afero.WriteFile(fs, fmt.Sprintf("%s/tests/tests.test", stepDir), []byte(`
					faketestbinary
					`), 0644)
				}

				if stubStep.RegionalResourcesExist {
					regionDir := filepath.Join(stepDir, "regional")
					fs.MkdirAll(regionDir, 0755)
					_ = afero.WriteFile(fs, fmt.Sprintf("%s/main.tf", regionDir), []byte(`
					faketestbinary
					`), 0644)
				}

				stubTracks[key] = track
			}
		}
	}

	// act
	flag.Parse()
	exitCode := m.Run()

	// Exit
	os.Exit(exitCode)
}

func TestGetTracksWithTargetAll_ShouldReturnCorrectTracks(t *testing.T) {
	// act
	mockTracks := sut.GatherTracks(config.Config{
		TargetAll: true,
	})

	// assert
	assert.Equal(t, stubTrackCount, len(mockTracks), "Two tracks should have been gathered")
	assert.Equal(t, stubTrackNameA, mockTracks[0].Name, "tracks should be correctly derived from directory")
	assert.Equal(t, 2, mockTracks[0].StepProgressionsCount, "StepProgressionsCount should be derived correctly based on steps")
	assert.Equal(t, 2, len(mockTracks[0].OrderedSteps[1]), "Track A Step Progression 1 should have 2 step(s)")
	assert.Equal(t, 1, len(mockTracks[0].OrderedSteps[2]), "Track A Step Progression 2 should have 1 step(s)")
	assert.Contains(t, stubTrackNameB, mockTracks[1].Name)

	for _, track := range mockTracks {
		totalStepSteps := 0
		totalStepsWithTestsCount := 0

		for progressionLevel, steps := range track.OrderedSteps {
			for _, step := range steps {
				totalStepSteps++
				if shouldHaveTests(stubTracks[track.Name].OrderedSteps[progressionLevel], step.Name) {
					require.True(t, step.TestsExist, fmt.Sprintf("Step %v should return true for steps existing", step.Name))
					totalStepsWithTestsCount++
				} else {
					require.False(t, step.TestsExist, fmt.Sprintf("Step %v should return false for tests existing", step.Name))
				}

				if shouldHaveRegionDeployment(stubTracks[track.Name].OrderedSteps[progressionLevel], step.Name) {
					require.True(t, step.RegionalResourcesExist, fmt.Sprintf("Step %v should return true for region deployment", step.Name))
				} else {
					require.False(t, step.RegionalResourcesExist, fmt.Sprintf("Step %v should return false for region deployment", step.Name))
				}
			}

			require.Equal(t, len(stubTracks[track.Name].OrderedSteps[progressionLevel]), len(steps), fmt.Sprintf("track %v progression level %v should have correct step count", track.Name, progressionLevel))

		}

		// important to match for handling channels
		require.Equal(t, totalStepSteps, track.StepsCount, "Track steps count should match total steps in OrderedSteps field")
		require.Equal(t, totalStepsWithTestsCount, track.StepsWithTestsCount, "Track StepsWithTestsCount should match total steps in OrderedSteps field")
		require.Contains(t, stubTracks, track.Name, "Track should be named correctly")
		require.Equal(t, stubTracks[track.Name].StepsWithTestsCount, track.StepsWithTestsCount, "Track StepsWithTestsCount should match total steps in OrderedSteps field")
	}
}

func TestGetTracksWithStepTarget_ShouldReturnCorrectTracks(t *testing.T) {
	stubStepWhitelist := []string{fmt.Sprintf("#core#%s#%s", stubTrackNameA, stubStepWithTests.Name), fmt.Sprintf("#core#%s#%s", stubTrackNameB, "b11")}
	// act
	mockTracks := sut.GatherTracks(config.Config{
		StepWhitelist: stubStepWhitelist,
		Stage:         "core",
	})

	// assert
	assert.Equal(t, 2, len(mockTracks), "Two tracks should have been gathered")
	stepCount := 0

	for _, track := range mockTracks {
		for _, steps := range track.OrderedSteps {
			for _, step := range steps {
				stepCount++
				require.Contains(t, stubStepWhitelist, fmt.Sprintf("#core#%s#%s", track.Name, step.Name), "Returned step %s should be in the step whitelist", step.Name)
			}
		}
	}
	// important to match for handling channels
	require.Equal(t, len(stubStepWhitelist), stepCount, "Track steps count should match total steps in defined in whitelist")
}

func shouldHaveTests(s []steps.Step, e string) bool {
	for _, a := range s {
		if a.Name == e {
			return a.TestsExist
		}
	}
	return false
}

func shouldHaveRegionDeployment(s []steps.Step, e string) bool {
	for _, a := range s {
		if a.Name == e {
			return a.RegionalResourcesExist
		}
	}
	return false
}

func TestExecuteTracks_ShouldHandleRegionalAutoDestroyWithRegionalOutputVariables(t *testing.T) {
	// arrange
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	stubPrimaryStepOutputVars := map[string]map[string]string{
		"step1": {
			"primary": "primary",
		},
	}

	stubRegionalStepOutputVars := map[string]map[string]string{
		"step1": {
			"regional": "regional",
		},
	}

	stubRegionalRegion := "regionalregion"
	stubPrimaryRegion := "primaryregion"

	deployTrackStub := map[string]tracks.Output{
		"track-a": {
			Name:                       "track-a",
			PrimaryStepOutputVariables: stubPrimaryStepOutputVars,
			Executions: []tracks.RegionExecution{
				{
					Output: tracks.ExecutionOutput{
						StepOutputVariables: stubPrimaryStepOutputVars,
					},
					Region:           stubPrimaryRegion,
					RegionDeployType: steps.PrimaryRegionDeployType,
				},
				{
					Output: tracks.ExecutionOutput{
						StepOutputVariables: stubRegionalStepOutputVars,
					},
					Region:           stubRegionalRegion,
					RegionDeployType: steps.RegionalRegionDeployType,
				},
			},
		},
		"track-b": {
			Name:                       "track-b",
			PrimaryStepOutputVariables: nil,
			Executions:                 nil,
		},
	}
	var destroyTrackASpy tracks.Execution
	deployTrackExecutionSpy := []tracks.Execution{}

	tracks.DeployTrack = func(execution tracks.Execution, cfg config.Config, t tracks.Track, out chan<- tracks.Output) {
		execution.Output.Name = t.Name
		deployTrackExecutionSpy = append(deployTrackExecutionSpy, execution)
		out <- deployTrackStub[t.Name]
		return
	}

	tracks.DestroyTrack = func(execution tracks.Execution, cfg config.Config, t tracks.Track, out chan<- tracks.Output) {
		if t.Name == "track-a" {
			destroyTrackASpy = execution
		}

		out <- tracks.Output{
			Name:                       "",
			PrimaryStepOutputVariables: nil,
			Executions:                 nil,
		}

		return
	}

	// act
	mockExecution := sut.ExecuteTracks(nil, config.Config{
		TargetAll:   true,
		SelfDestroy: true,
	})

	require.NotNil(t, mockExecution)

	for _, executionSpy := range deployTrackExecutionSpy {
		require.Contains(t, []string{"track-a", "track-b"}, executionSpy.Output.Name, "Should execute correct tracks")
	}
	require.Equal(t, stubPrimaryStepOutputVars, destroyTrackASpy.DefaultExecutionStepOutputVariables[steps.PrimaryRegionDeployType.String()+"-"+stubPrimaryRegion], "Should pass primary step output vars to destroy")
	require.Equal(t, stubRegionalStepOutputVars, destroyTrackASpy.DefaultExecutionStepOutputVariables[steps.RegionalRegionDeployType.String()+"-"+stubRegionalRegion], "Should pass regional region specific step output vars to destroy")
}

func TestExecuteDeployTrack_ShouldExecuteCorrectStepsAndRegions(t *testing.T) {
	// arrange
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	stubStepperFactory := mocks.NewMockStepperFactory(ctrl)

	var test = map[string]struct {
		stubExecutedFailCount     int
		stubExecutedTestFailCount int
		stubRegionalDeployment    bool
		regionGroup               string
		stubTargetRegions         []string
		expectedCallCount         int
	}{
		"ShouldExecutePrimaryRegionOnceWithNoRegionalResources": {
			stubExecutedFailCount:     0,
			stubExecutedTestFailCount: 0,
			stubRegionalDeployment:    false,
			regionGroup:               "us",
			stubTargetRegions:         []string{"us-east-1"},
			expectedCallCount:         1,
		},
		"ShouldExecutePrimaryRegionTwiceWithRegionalResources": {
			stubExecutedFailCount:     0,
			stubExecutedTestFailCount: 0,
			stubRegionalDeployment:    true,
			regionGroup:               "us",
			stubTargetRegions:         []string{"us-east-1"},
			expectedCallCount:         2,
		},
		"ShouldExecuteN+PrimaryTimesWithRegionalResourcesTargetingNRegions": {
			stubExecutedFailCount:     0,
			stubExecutedTestFailCount: 0,
			stubRegionalDeployment:    true,
			regionGroup:               "us",
			stubTargetRegions:         []string{"us-east-1", "us-east-2"},
			expectedCallCount:         3,
		},
		"ShouldNotExecuteTargetRegionsWhenPrimaryHasFail": {
			stubExecutedFailCount:     1,
			stubExecutedTestFailCount: 0,
			stubRegionalDeployment:    true,
			regionGroup:               "us",
			stubTargetRegions:         []string{"us-east-1", "us-east-2"},
			expectedCallCount:         1,
		},
	}

	executionParams := []tracks.RegionExecution{}

	for name, test := range test {
		t.Run(name, func(t *testing.T) {

			var callCount int
			tracks.DeployTrackRegion = func(in <-chan tracks.RegionExecution, out chan<- tracks.RegionExecution) {
				regionExecution := <-in
				callCount++

				// sleep to ensure DefaultStepOutputVariables do not overwrite each other based on concurrency timing
				if regionExecution.RegionDeployType == steps.RegionalRegionDeployType && callCount == 2 {
					time.Sleep(100)
				}

				executionParams = append(executionParams, regionExecution)

				regionExecution.Output = tracks.ExecutionOutput{
					FailureCount:        test.stubExecutedFailCount,
					FailedTestCount:     test.stubExecutedTestFailCount,
					StepOutputVariables: regionExecution.DefaultStepOutputVariables,
				}

				regionExecution.Output.StepOutputVariables["test-regional"] = map[string]string{
					"region": regionExecution.Region,
				}

				out <- regionExecution
				return
			}

			trackChan := make(chan tracks.Output, 1)

			// act
			tracks.ExecuteDeployTrack(tracks.Execution{
				Logger:         logger,
				Fs:             fs,
				Output:         tracks.ExecutionOutput{},
				StepperFactory: stubStepperFactory,
			}, config.Config{
				GaiaTargetRegions: test.stubTargetRegions,
				GaiaRegionGroup:   test.regionGroup,
				CSP:               "aws",
			}, tracks.Track{
				RegionalDeployment: test.stubRegionalDeployment,
			}, trackChan)

			mockOutput := <-trackChan

			require.Len(t, mockOutput.Executions, callCount)

			for _, exec := range mockOutput.Executions {
				require.Equal(t, exec.Region, exec.Output.StepOutputVariables["test-regional"]["region"], "Region variables should stay scoped to executing region function")
			}

			require.Equal(t, map[string]map[string]string(nil), executionParams[0].Output.StepOutputVariables, "Primary execution should start with no incoming previous step variables")
			require.Equal(t, test.stubExecutedFailCount*callCount, mockOutput.Executions[0].Output.FailureCount, "Should correctly set failed step count")
			require.Equal(t, steps.PrimaryRegionDeployType, executionParams[0].RegionDeployType, "First execution should be primary region")
			require.Equal(t, test.expectedCallCount, callCount, "Should call DeployTrackRegion() the expected amount of times")
		})
	}
}

func TestAddToTrackOutput(t *testing.T) {
	stepOutputVariables := make(map[string]interface{})
	stepOutputVariables["resource_name"] = "my-cool-resource"
	stepOutputVariables["resource_id"] = "resource/my-cool-resource"

	stepOutput := steps.StepOutput{
		OutputVariables: stepOutputVariables,
		StepName:        "cool_step1",
	}

	trackOutputVars := make(map[string]map[string]string)

	mockPrevStepVars := tracks.AppendTrackOutput(trackOutputVars, stepOutput)

	require.Equal(t, "my-cool-resource", mockPrevStepVars[stepOutput.StepName]["resource_name"], "The track output should have the correct key and value set")
	require.Equal(t, "resource/my-cool-resource", mockPrevStepVars[stepOutput.StepName]["resource_id"], "The track output should have the correct key and value set")
}

func TestAppendTrackOutput_WithRegionalStepDeploymentOutput(t *testing.T) {
	stepOutputVariables := make(map[string]interface{})
	stepOutputVariables["resource_name"] = "my-cool-resource"
	stepOutputVariables["resource_id"] = "resource/my-cool-resource"

	stepOutput := steps.StepOutput{
		OutputVariables:  stepOutputVariables,
		StepName:         "cool_step1",
		RegionDeployType: steps.RegionalRegionDeployType,
	}

	trackOutputVars := make(map[string]map[string]string)

	mockPrevStepVars := tracks.AppendTrackOutput(trackOutputVars, stepOutput)

	key := fmt.Sprintf("%s-%s", stepOutput.StepName, steps.RegionalRegionDeployType.String())

	for k, v := range stepOutputVariables {
		require.Equal(t, v, mockPrevStepVars[key][k], "The track output should match the stubbed key value: %s, %s", k, v)
	}
}

type spyExecuteStep struct {
	OutputVars map[string]map[string]string
	StepName   string
}

func TestExecuteDeployTrackRegion_ShouldPassRegionalVariables(t *testing.T) {
	primaryOutChan := make(chan tracks.RegionExecution, 1)
	primaryInChan := make(chan tracks.RegionExecution, 1)

	trackOutputVars := []spyExecuteStep{}

	stubStepP1OutputVars := map[string]interface{}{
		"var": "var",
	}

	tracks.ExecuteStep = func(stepperFactory steps.StepperFactory, region string, regionDeployType steps.RegionDeployType, entry *logrus.Entry, fs afero.Fs, defaultStepOutputVariables map[string]map[string]string, stepProgression int,
		s steps.Step, out chan<- steps.Step, destroy bool) {
		trackOutputVars = append(trackOutputVars, spyExecuteStep{
			OutputVars: defaultStepOutputVariables,
			StepName:   s.Name,
		})

		if s.ProgressionLevel == 1 {
			s.Output.OutputVariables = stubStepP1OutputVars
			s.Output.Status = steps.Success
			s.Output.StepName = s.Name
		}
		s.Output.RegionDeployType = regionDeployType
		s.Output.Region = region
		out <- s
		return
	}

	regionalExecution := tracks.RegionExecution{
		TrackName:                  "",
		TrackDir:                   "",
		TrackStepProgressionsCount: 2,
		TrackStepsWithTestsCount:   0,
		TrackOrderedSteps: map[int][]steps.Step{
			1: {
				{
					Name:                   "step1_p1",
					ProgressionLevel:       1,
					RegionalResourcesExist: true,
				},
				{
					Name:                   "step2_p1",
					ProgressionLevel:       1,
					RegionalResourcesExist: true,
				},
			},
			2: {
				{
					Name:                   "step_p2",
					RegionalResourcesExist: true,
				},
			},
		},
		Logger:           logger,
		Fs:               fs,
		Output:           tracks.ExecutionOutput{},
		Region:           "",
		RegionDeployType: steps.RegionalRegionDeployType,
		StepperFactory:   nil,
		DefaultStepOutputVariables: map[string]map[string]string{
			"step1_p1": {
				"primaryvarkey": "primaryvarvalue",
			},
		},
	}

	go tracks.ExecuteDeployTrackRegion(primaryInChan, primaryOutChan)
	primaryInChan <- regionalExecution

	primaryTrackExecution := <-primaryOutChan

	require.NotNil(t, primaryTrackExecution)

	expectedStepP1OutputVarsStrings := map[string]map[string]string{
		"step1_p1-regional": {
			"var": "var",
		},
		"step2_p1-regional": {
			"var": "var",
		},
		"step1_p1": {
			"primaryvarkey": "primaryvarvalue",
		},
	}

	var trackOutputVarsSpyStepP2 map[string]map[string]string
	for _, outputVars := range trackOutputVars {
		if outputVars.StepName == "step_p2" {
			trackOutputVarsSpyStepP2 = outputVars.OutputVars
		}
	}

	require.Equal(t, expectedStepP1OutputVarsStrings["step1_p1"], trackOutputVarsSpyStepP2["step1_p1"], "Primary execution output vars should be passed to regional")
	require.Equal(t, expectedStepP1OutputVarsStrings["step1_p1-regional"], trackOutputVarsSpyStepP2["step1_p1-regional"], "Output vars should be passed from previous progression steps to current progression steps")
	require.Equal(t, expectedStepP1OutputVarsStrings["step2_p1-regional"], trackOutputVarsSpyStepP2["step2_p1-regional"], "Output vars should be passed from previous progression steps to current progression steps")

}

func TestExecuteDeployTrackRegion_ShouldNotExecuteSecondProgressionWhenFirstFails(t *testing.T) {
	primaryOutChan := make(chan tracks.RegionExecution, 1)
	primaryInChan := make(chan tracks.RegionExecution, 1)

	executeStepSpy := map[string]steps.Step{}

	tracks.ExecuteStep = func(stepperFactory steps.StepperFactory, region string, regionDeployType steps.RegionDeployType, entry *logrus.Entry, fs afero.Fs, defaultStepOutputVariables map[string]map[string]string, stepProgression int,
		s steps.Step, out chan<- steps.Step, destroy bool) {
		executeStepSpy[s.Name] = s

		s.Output = steps.StepOutput{
			Status: steps.Fail,
		}
		out <- s
		return
	}
	regionalExecution := tracks.RegionExecution{
		Logger:                     logger,
		Fs:                         fs,
		Output:                     tracks.ExecutionOutput{},
		TrackStepProgressionsCount: 2,
		TrackOrderedSteps: map[int][]steps.Step{
			1: {
				{
					ID:        "",
					Name:      "step_p1",
					TrackName: "",
					Dir:       "",
				},
			},
			2: {
				{
					ID:        "",
					Name:      "step_p2",
					TrackName: "",
					Dir:       "",
				},
			},
		},
	}

	go tracks.ExecuteDeployTrackRegion(primaryInChan, primaryOutChan)
	primaryInChan <- regionalExecution
	primaryTrackExecution := <-primaryOutChan

	require.NotNil(t, primaryTrackExecution)
	require.Len(t, executeStepSpy, 1, "Should not execute the second progression step with a failure in first progression")
}