import 'package:flutter/material.dart';

import '../theme/tokens.dart';

/// Page title block with optional subtitle and right-aligned actions.
/// Wraps actions below the title on narrow screens.
class PageHeader extends StatelessWidget {
  const PageHeader({
    super.key,
    required this.title,
    this.subtitle,
    this.actions = const [],
    this.leading,
  });

  final String title;
  final String? subtitle;
  final List<Widget> actions;
  final Widget? leading;

  @override
  Widget build(BuildContext context) {
    final titleBlock = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(title, style: context.tt.headlineMedium),
        if (subtitle != null) ...[
          const SizedBox(height: AppSpace.xs),
          Text(
            subtitle!,
            style: context.tt.bodyLarge?.copyWith(
              color: context.jc.textSecondary,
            ),
          ),
        ],
      ],
    );
    return LayoutBuilder(
      builder: (context, c) {
        final narrow = c.maxWidth < AppBreakpoints.compact;
        final head = Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            if (leading != null) ...[
              leading!,
              const SizedBox(width: AppSpace.md),
            ],
            Expanded(child: titleBlock),
            if (!narrow && actions.isNotEmpty) ...[
              const SizedBox(width: AppSpace.lg),
              Wrap(
                spacing: AppSpace.sm,
                runSpacing: AppSpace.sm,
                children: actions,
              ),
            ],
          ],
        );
        if (!narrow || actions.isEmpty) return head;
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            head,
            const SizedBox(height: AppSpace.md),
            Wrap(
              spacing: AppSpace.sm,
              runSpacing: AppSpace.sm,
              children: actions,
            ),
          ],
        );
      },
    );
  }
}

/// Standard scrollable page body: max width, responsive padding, header + children.
class PageScaffold extends StatelessWidget {
  const PageScaffold({
    super.key,
    required this.header,
    required this.children,
    this.maxWidth = AppSpace.contentMaxWidth,
  });

  final Widget header;
  final List<Widget> children;
  final double maxWidth;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, c) {
        final pad = c.maxWidth < AppBreakpoints.compact
            ? AppSpace.lg
            : AppSpace.xxl;
        return SingleChildScrollView(
          padding: EdgeInsets.fromLTRB(pad, pad, pad, AppSpace.xxxl),
          child: Center(
            child: ConstrainedBox(
              constraints: BoxConstraints(maxWidth: maxWidth),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  header,
                  const SizedBox(height: AppSpace.xl),
                  ...children,
                ],
              ),
            ),
          ),
        );
      },
    );
  }
}
