import 'package:flutter/material.dart';

import '../../../core/api/models.dart';
import '../../../core/format.dart';
import '../../../core/theme/tokens.dart';
import '../../../core/widgets/widgets.dart';
import '../orders_metrics.dart';

/// Width below which the table collapses into stacked cards.
const double _tableMin = 840;

/// Order history: a table on wide screens, cards on narrow ones.
class OrdersTable extends StatelessWidget {
  const OrdersTable({super.key, required this.orders, required this.onOpen});

  final List<OrderSummary> orders;
  final ValueChanged<OrderSummary> onOpen;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, c) {
        if (c.maxWidth < _tableMin) {
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              for (final o in orders) ...[
                _OrderCard(order: o, onTap: () => onOpen(o)),
                const SizedBox(height: AppSpace.sm),
              ],
            ],
          );
        }
        final jc = context.jc;
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Container(
              padding: const EdgeInsets.symmetric(
                horizontal: AppSpace.lg,
                vertical: AppSpace.sm + 2,
              ),
              decoration: BoxDecoration(
                color: jc.surfaceMuted,
                border: Border(bottom: BorderSide(color: jc.border)),
              ),
              child: const _Cells(
                order: Text('ORDER'),
                vendors: Text('VENDORS'),
                items: Text('ITEMS', textAlign: TextAlign.right),
                approver: Text('APPROVAL'),
                total: Text('TOTAL', textAlign: TextAlign.right),
                status: Text('STATUS'),
                header: true,
              ),
            ),
            for (final (i, o) in orders.indexed)
              _OrderRow(
                order: o,
                onTap: () => onOpen(o),
                divider: i < orders.length - 1,
              ),
          ],
        );
      },
    );
  }
}

class _Cells extends StatelessWidget {
  const _Cells({
    required this.order,
    required this.vendors,
    required this.items,
    required this.approver,
    required this.total,
    required this.status,
    this.header = false,
  });

  final Widget order;
  final Widget vendors;
  final Widget items;
  final Widget approver;
  final Widget total;
  final Widget status;
  final bool header;

  @override
  Widget build(BuildContext context) {
    final row = Row(
      children: [
        Expanded(flex: 30, child: order),
        const SizedBox(width: AppSpace.md),
        Expanded(flex: 22, child: vendors),
        const SizedBox(width: AppSpace.md),
        Expanded(flex: 7, child: items),
        const SizedBox(width: AppSpace.lg),
        Expanded(flex: 16, child: approver),
        const SizedBox(width: AppSpace.md),
        Expanded(flex: 13, child: total),
        const SizedBox(width: AppSpace.lg),
        Expanded(
          flex: 20,
          child: Align(
            alignment: Alignment.centerLeft,
            child: FittedBox(
              fit: BoxFit.scaleDown,
              alignment: Alignment.centerLeft,
              child: status,
            ),
          ),
        ),
      ],
    );
    if (!header) return row;
    return DefaultTextStyle.merge(
      style: context.tt.labelSmall?.copyWith(letterSpacing: 0.6),
      child: row,
    );
  }
}

Widget _approval(BuildContext context, OrderSummary o) {
  final jc = context.jc;
  if (o.approver != null) {
    return Row(
      children: [
        Icon(Icons.verified_rounded, size: 16, color: jc.success),
        const SizedBox(width: AppSpace.xs),
        Flexible(
          child: Text(
            o.approver!.name,
            style: context.tt.bodyMedium,
            overflow: TextOverflow.ellipsis,
          ),
        ),
      ],
    );
  }
  if (o.request.decision == Decision.autoApprove) {
    return Row(
      children: [
        Icon(Icons.bolt_rounded, size: 16, color: jc.brand),
        const SizedBox(width: AppSpace.xs),
        Flexible(
          child: Text(
            'Auto-approved',
            style: context.tt.bodyMedium?.copyWith(color: jc.textSecondary),
            overflow: TextOverflow.ellipsis,
          ),
        ),
      ],
    );
  }
  return Text(
    o.request.decision.label,
    style: context.tt.bodyMedium?.copyWith(color: jc.textMuted),
    overflow: TextOverflow.ellipsis,
  );
}

String _vendorsText(OrderSummary o) {
  if (o.vendors.isEmpty) return '—';
  if (o.vendors.length <= 2) return o.vendors.join(', ');
  return '${o.vendors.take(2).join(', ')} +${o.vendors.length - 2}';
}

class _OrderRow extends StatefulWidget {
  const _OrderRow({
    required this.order,
    required this.onTap,
    required this.divider,
  });
  final OrderSummary order;
  final VoidCallback onTap;
  final bool divider;

  @override
  State<_OrderRow> createState() => _OrderRowState();
}

class _OrderRowState extends State<_OrderRow> {
  bool _hover = false;

  @override
  Widget build(BuildContext context) {
    final jc = context.jc;
    final o = widget.order;
    return MouseRegion(
      onEnter: (_) => setState(() => _hover = true),
      onExit: (_) => setState(() => _hover = false),
      child: InkWell(
        key: Key('order-row-${o.request.id}'),
        onTap: widget.onTap,
        child: Container(
          padding: const EdgeInsets.symmetric(
            horizontal: AppSpace.lg,
            vertical: AppSpace.md,
          ),
          decoration: BoxDecoration(
            color: _hover ? jc.surfaceMuted : null,
            border: widget.divider
                ? Border(bottom: BorderSide(color: jc.border))
                : null,
          ),
          child: _Cells(
            order: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  OrdersMetrics.title(o),
                  style: context.tt.titleSmall,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
                const SizedBox(height: AppSpace.xxs),
                Text(
                  '${Fmt.date(o.request.createdAt)} · '
                  '${o.requester?.name ?? OrdersMetrics.shortId(o.request.id)}',
                  style: context.tt.bodySmall,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ],
            ),
            vendors: Text(
              _vendorsText(o),
              style: context.tt.bodyMedium,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
            items: Text(
              '${o.itemCount}',
              textAlign: TextAlign.right,
              style: context.tt.bodyMedium?.copyWith(
                fontFeatures: const [FontFeature.tabularFigures()],
              ),
            ),
            approver: _approval(context, o),
            total: Align(
              alignment: Alignment.centerRight,
              child: MoneyText(
                OrdersMetrics.orderTotalCents(o),
                style: context.tt.titleSmall,
              ),
            ),
            status: StatusChip.request(o.request.status),
          ),
        ),
      ),
    );
  }
}

class _OrderCard extends StatelessWidget {
  const _OrderCard({required this.order, required this.onTap});
  final OrderSummary order;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final o = order;
    return AppCard(
      key: Key('order-row-${o.request.id}'),
      onTap: onTap,
      padding: const EdgeInsets.all(AppSpace.md + 2),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Text(
                  OrdersMetrics.title(o),
                  style: context.tt.titleSmall,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              const SizedBox(width: AppSpace.sm),
              MoneyText(
                OrdersMetrics.orderTotalCents(o),
                style: context.tt.titleSmall,
              ),
            ],
          ),
          const SizedBox(height: AppSpace.xs),
          Text(
            '${Fmt.date(o.request.createdAt)} · ${_vendorsText(o)} · '
            '${o.itemCount} ${o.itemCount == 1 ? 'item' : 'items'}',
            style: context.tt.bodySmall,
          ),
          const SizedBox(height: AppSpace.sm),
          Row(
            children: [
              StatusChip.request(o.request.status),
              const SizedBox(width: AppSpace.sm),
              Expanded(child: _approval(context, o)),
            ],
          ),
        ],
      ),
    );
  }
}
