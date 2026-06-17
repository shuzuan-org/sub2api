package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/domain"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SubscriptionPlan holds the schema definition for the SubscriptionPlan entity.
type SubscriptionPlan struct {
	ent.Schema
}

func (SubscriptionPlan) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "subscription_plans"},
	}
}

func (SubscriptionPlan) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
		mixins.SoftDeleteMixin{},
	}
}

func (SubscriptionPlan) Fields() []ent.Field {
	return []ent.Field{
		// 唯一约束通过部分索引实现（WHERE deleted_at IS NULL），支持软删除后重用
		field.String("name").
			MaxLen(100).
			NotEmpty(),
		field.String("description").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.String("visibility").
			MaxLen(20).
			Default(domain.VisibilityPublic),
		field.String("status").
			MaxLen(20).
			Default(domain.StatusActive),

		// 额度限制（nullable = 无限制）
		field.Float("daily_limit_usd").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Float("weekly_limit_usd").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		field.Float("monthly_limit_usd").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),

		field.Int("default_validity_days").
			Default(30),

		// 定价（nullable = 不可自助购买）
		field.Float("price").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),

		field.Int("sort_order").
			Default(0),
	}
}

func (SubscriptionPlan) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("subscriptions", UserSubscription.Type),
		edge.To("redeem_codes", RedeemCode.Type),
		// 反向：哪些 subscriber 分组绑定了此计划
		edge.From("visible_groups", Group.Type).
			Ref("visible_plans").
			Through("group_visible_plans", GroupVisiblePlan.Type),
	}
}

func (SubscriptionPlan) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("visibility", "status"),
		index.Fields("status"),
		index.Fields("sort_order"),
		index.Fields("deleted_at"),
	}
}
