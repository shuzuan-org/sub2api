package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// GroupVisiblePlan holds the edge schema definition for the group_visible_plans relationship.
// It binds a "subscriber"-visibility group to the subscription plans whose active holders may see/bind it.
type GroupVisiblePlan struct {
	ent.Schema
}

func (GroupVisiblePlan) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "group_visible_plans"},
		// Composite primary key: (group_id, plan_id).
		field.ID("group_id", "plan_id"),
	}
}

func (GroupVisiblePlan) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("group_id"),
		field.Int64("plan_id"),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (GroupVisiblePlan) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("group", Group.Type).
			Unique().
			Required().
			Field("group_id"),
		edge.To("plan", SubscriptionPlan.Type).
			Unique().
			Required().
			Field("plan_id"),
	}
}

func (GroupVisiblePlan) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("plan_id"),
	}
}
