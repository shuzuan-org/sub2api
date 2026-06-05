package schema

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ChannelInviteBatch holds the schema definition for the ChannelInviteBatch entity.
//
// 渠道邀请码批次：一个批次包含多个邀请码，共享相同的优惠金额、分组配置和有效期。
//
// 删除策略：硬删除（批次删除时级联删除 codes 和 batch_groups）
type ChannelInviteBatch struct {
	ent.Schema
}

func (ChannelInviteBatch) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "channel_invite_batches"},
	}
}

func (ChannelInviteBatch) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			MaxLen(100).
			NotEmpty().
			Comment("批次名称"),
		field.Float("bonus_amount").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Default(0).
			Comment("赠送余额金额（U）"),
		field.Int("max_uses_per_code").
			Default(1).
			Comment("每码最大使用次数，0=无限"),
		field.Time("start_time").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("有效期开始时间"),
		field.Time("end_time").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("有效期结束时间"),
		field.String("status").
			MaxLen(20).
			Default(domain.ChannelInviteBatchStatusActive).
			Comment("状态: active, disabled"),
		field.String("notes").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Comment("备注"),
		field.Int64("created_by").
			Comment("创建者（渠道合作方）用户ID"),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (ChannelInviteBatch) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("creator", User.Type).
			Ref("channel_invite_batches").
			Field("created_by").
			Required().
			Unique(),
		edge.To("codes", ChannelInviteCode.Type),
		edge.To("batch_groups", ChannelInviteBatchGroup.Type),
		edge.To("usages", ChannelInviteCodeUsage.Type),
	}
}

func (ChannelInviteBatch) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"),
		index.Fields("created_by"),
	}
}
