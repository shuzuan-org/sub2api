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

// ChannelInviteCodeUsage holds the schema definition for the ChannelInviteCodeUsage entity.
//
// 渠道邀请码使用记录：记录用户兑换邀请码的情况。
type ChannelInviteCodeUsage struct {
	ent.Schema
}

func (ChannelInviteCodeUsage) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "channel_invite_code_usages"},
	}
}

func (ChannelInviteCodeUsage) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("code_id").
			Comment("邀请码ID"),
		field.Int64("batch_id").
			Comment("批次ID"),
		field.Int64("user_id").
			Comment("兑换用户ID"),
		field.Bool("bonus_granted").
			Default(false).
			Comment("奖励是否已发放"),
		field.Time("bonus_granted_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("奖励发放时间"),
		field.Time("claimed_at").
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("兑换时间"),
	}
}

func (ChannelInviteCodeUsage) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("code", ChannelInviteCode.Type).
			Ref("usages").
			Field("code_id").
			Required().
			Unique(),
		edge.From("batch", ChannelInviteBatch.Type).
			Ref("usages").
			Field("batch_id").
			Required().
			Unique(),
		edge.From("user", User.Type).
			Ref("channel_invite_usages").
			Field("user_id").
			Required().
			Unique(),
	}
}

func (ChannelInviteCodeUsage) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("code_id", "user_id").Unique(),
		index.Fields("code_id"),
		index.Fields("batch_id"),
		index.Fields("user_id"),
		index.Fields("user_id", "bonus_granted"),
	}
}
