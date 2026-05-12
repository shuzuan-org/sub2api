package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AlipayOrder holds the schema definition for the AlipayOrder entity.
//
// 删除策略：硬删除
// 订单记录通过 status 字段追踪生命周期。
type AlipayOrder struct {
	ent.Schema
}

func (AlipayOrder) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "alipay_orders"},
	}
}

func (AlipayOrder) Fields() []ent.Field {
	return []ent.Field{
		// 业务订单号，格式: AP{timestamp}{random6}，用于支付宝 out_trade_no
		field.String("order_no").
			MaxLen(64).
			NotEmpty().
			Unique(),
		// 下单用户 ID
		field.Int64("user_id"),
		// 套餐 ID（来自 Setting 表 alipay_packages 中的 id；自定义金额时为 0）
		field.Int("package_id"),
		// 支付金额（人民币，单位：分）
		field.Int("cny_fee"),
		// 到账金额（U 代币；字段名 usd_amount 保留历史兼容）
		field.Float("usd_amount").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}),
		// 订单状态：pending / paid / expired / refunded
		field.String("status").
			MaxLen(20).
			Default("pending"),
		// 支付宝交易号（支付成功后填入）
		field.String("alipay_trade_no").
			MaxLen(64).
			Optional().
			Nillable(),
		// 支付宝返回的二维码链接（当面付 qr_code）
		field.String("qr_code").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Optional().
			Nillable(),
		// 订单过期时间（30分钟）
		field.Time("expires_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		// 支付成功时间
		field.Time("paid_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		// 支付宝回调原始数据（审计用）
		field.String("notify_data").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Optional().
			Nillable(),
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

func (AlipayOrder) Edges() []ent.Edge {
	return nil
}

func (AlipayOrder) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
		index.Fields("order_no"),
		index.Fields("status"),
		index.Fields("user_id", "created_at"),
	}
}
