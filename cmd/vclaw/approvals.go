package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"vclaw/internal/monitoring"
)

func runApprovals(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("vclaw approvals", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	status := fs.String("status", "", "optional status filter: pending, approved, rejected, expired, or revised")
	limit := fs.Int("limit", 20, "maximum number of approvals to show")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if value := strings.TrimSpace(*status); value != "" && !isValidApprovalStatus(value) {
		return fmt.Errorf("invalid approval status %q", value)
	}

	approvals, err := monitoring.QueryApprovals(ctx, strings.TrimSpace(os.Getenv("DATABASE_URL")), monitoring.ApprovalQuery{
		Status: strings.TrimSpace(*status),
		Limit:  *limit,
	})
	if err != nil {
		return err
	}
	if len(approvals) == 0 {
		fmt.Println("No approval requests found.")
		return nil
	}
	for _, approval := range approvals {
		printApprovalRecord(approval)
	}
	return nil
}

func isValidApprovalStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pending", "approved", "rejected", "expired", "revised":
		return true
	default:
		return false
	}
}

func printApprovalRecord(record monitoring.ApprovalRecord) {
	fmt.Printf("approvalId: %s\n", record.ApprovalID)
	fmt.Printf("tool:       %s\n", record.ToolName)
	fmt.Printf("risk:       %s\n", record.RiskLevel)
	fmt.Printf("status:     %s\n", record.Status)
	fmt.Printf("created:    %s\n", record.CreatedAt.Format(time.RFC3339))
	if record.DecidedAt != nil {
		fmt.Printf("decided:    %s\n", record.DecidedAt.Format(time.RFC3339))
	}
	fmt.Println()
}
