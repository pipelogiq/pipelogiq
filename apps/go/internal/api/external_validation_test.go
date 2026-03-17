package api

import (
	"testing"

	"pipelogiq/internal/types"
)

func TestValidateAppendStagesRequest(t *testing.T) {
	t.Run("empty stages invalid", func(t *testing.T) {
		errs := validateAppendStagesRequest(types.AppendStagesRequest{})
		if len(errs) == 0 {
			t.Fatalf("expected validation errors for empty stages")
		}
	})

	t.Run("invalid stage fields and options", func(t *testing.T) {
		neg := -1
		zero := 0
		errs := validateAppendStagesRequest(types.AppendStagesRequest{
			Stages: []types.StageCreate{
				{
					Name:         "",
					StageHandler: "",
					Options: &types.StageOptions{
						RetryInterval: &neg,
						MaxRetries:    &neg,
						TimeOut:       &zero,
					},
				},
			},
		})
		if len(errs) < 5 {
			t.Fatalf("expected multiple validation errors, got %v", errs)
		}
	})
}

func TestValidateResumeStageRequest(t *testing.T) {
	t.Run("approved false requires reason", func(t *testing.T) {
		errs := validateResumeStageRequest(types.ResumeStageRequest{
			Approved: false,
		})
		if len(errs) == 0 {
			t.Fatalf("expected validation error for missing rejectionReason")
		}
	})

	t.Run("approved true allows empty reason", func(t *testing.T) {
		errs := validateResumeStageRequest(types.ResumeStageRequest{
			Approved: true,
		})
		if len(errs) != 0 {
			t.Fatalf("did not expect validation errors, got %v", errs)
		}
	})
}
