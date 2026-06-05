package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ChannelInviteBatchGroup holds the schema definition for the ChannelInviteBatchGroup entity.
//
// 批次-分组关联表：一个批次可关联多个目标分组。
type ChannelInviteBatchGroup struct {
	ent.Schema
}

func (ChannelInviteBatchGroup) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "channel_invite_batch_groups"},
	}
}

func (ChannelInviteBatchGroup) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("batch_id").
			Comment("批次ID"),
		field.Int64("group_id").
			Comment("分组ID"),
	}
}

func (ChannelInviteBatchGroup) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("batch", ChannelInviteBatch.Type).
			Ref("batch_groups").
			Field("batch_id").
			Required().
			Unique(),
		edge.From("group", Group.Type).
			Ref("channel_invite_batch_groups").
			Field("group_id").
			Required().
			Unique(),
	}
}

func (ChannelInviteBatchGroup) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("batch_id", "group_id").Unique(),
		index.Fields("batch_id"),
		index.Fields("group_id"),
	}
}
