package delegation

import "time"

type ParallelPlan struct {
	PlanID         string
	OwnerID        string
	GoalID         string
	TaskStepID     string
	Status         string
	DefinitionHash string
	Content        string
	Revision       int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ParallelNode struct {
	PlanID     string
	NodeID     string
	OwnerID    string
	Role       string
	Status     string
	Attempt    int
	ResultID   string
	ErrorChain string
	Content    string
	Revision   int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type ParallelAggregate struct {
	AggregateID string
	PlanID      string
	OwnerID     string
	Status      string
	Content     string
	CreatedAt   time.Time
}

type ParallelPlanBundle struct {
	Plan  ParallelPlan
	Nodes []ParallelNode
	Event Event
}

type ParallelNodeTransition struct {
	OwnerID          string
	PlanID           string
	NodeID           string
	ExpectedRevision int64
	Role             string
	Status           string
	Attempt          int
	ResultID         string
	ErrorChain       string
	Content          string
	UpdatedAt        time.Time
	Event            Event
}

type ParallelPlanCompletion struct {
	OwnerID          string
	PlanID           string
	ExpectedRevision int64
	Status           string
	Aggregate        ParallelAggregate
	CompletedAt      time.Time
	Event            Event
}
