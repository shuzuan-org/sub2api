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

// ChannelInviteCode holds the schema definition for the ChannelInviteCode entity.
//
// 渠道邀请码（个体码）：属于某个批次，记录使用次数。
//
// 删除策略：硬删除（随批次级联删除）
type ChannelInviteCode struct {
	ent.Schema
}

func (ChannelInviteCode) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "channel_invite_codes"},
	}
}

func (ChannelInviteCode) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("batch_id").
			Comment("所属批次ID"),
		field.String("code").
			MaxLen(32).
			NotEmpty().
			Unique().
			Comment("邀请码"),
		field.String("status").
			MaxLen(20).
			Default(domain.ChannelInviteCodeStatusUnused).
			Comment("状态: unused, used, expired"),
		field.Int("max_uses").
			Default(1).
			Comment("最大使用次数"),
		field.Int("used_count").
			Default(0).
			Comment("已使用次数"),
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

func (ChannelInviteCode) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("batch", ChannelInviteBatch.Type).
			Ref("codes").
			Field("batch_id").
			Required().
			Unique(),
		edge.To("usages", ChannelInviteCodeUsage.Type),
	}
}

func (ChannelInviteCode) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("batch_id"),
		index.Fields("status"),
		// code 字段已在 Fields() 中声明 Unique()，无需重复索引
	}
}
