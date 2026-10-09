import 'dart:ui' as ui;

import 'package:flutter/material.dart';

/// Frosted-glass panel used for buttons and cards.
class Glass extends StatelessWidget {
  const Glass({
    super.key,
    required this.child,
    this.radius = 24,
    this.tint = Colors.white,
    this.tintAlpha = 0.07,
    this.borderAlpha = 0.14,
    this.glow,
    this.padding,
  });

  final Widget child;
  final double radius;
  final Color tint;
  final double tintAlpha;
  final double borderAlpha;
  final Color? glow;
  final EdgeInsetsGeometry? padding;

  @override
  Widget build(BuildContext context) {
    final br = BorderRadius.circular(radius);
    return DecoratedBox(
      decoration: BoxDecoration(
        borderRadius: br,
        boxShadow: [
          if (glow != null)
            BoxShadow(
              color: glow!.withValues(alpha: 0.35),
              blurRadius: 32,
              spreadRadius: -4,
            ),
        ],
      ),
      child: ClipRRect(
        borderRadius: br,
        child: BackdropFilter(
          filter: ui.ImageFilter.blur(sigmaX: 22, sigmaY: 22),
          child: Container(
            padding: padding,
            decoration: BoxDecoration(
              borderRadius: br,
              gradient: LinearGradient(
                begin: Alignment.topLeft,
                end: Alignment.bottomRight,
                colors: [
                  tint.withValues(alpha: tintAlpha * 1.5),
                  tint.withValues(alpha: tintAlpha * 0.6),
                ],
              ),
              border: Border.all(
                color: Colors.white.withValues(alpha: borderAlpha),
                width: 0.8,
              ),
            ),
            child: child,
          ),
        ),
      ),
    );
  }
}

/// Round glass icon button with a caption.
class GlassIconButton extends StatelessWidget {
  const GlassIconButton({
    super.key,
    required this.icon,
    required this.label,
    required this.onTap,
    this.active = false,
    this.danger = false,
  });

  final IconData icon;
  final String label;
  final VoidCallback? onTap;
  final bool active;
  final bool danger;

  @override
  Widget build(BuildContext context) {
    final enabled = onTap != null;
    final tint = danger
        ? const Color(0xFFFF4D6D)
        : active
        ? const Color(0xFF3EE7FF)
        : Colors.white;
    return Semantics(
      button: true,
      label: label,
      child: AnimatedOpacity(
        duration: const Duration(milliseconds: 300),
        opacity: enabled ? 1 : 0.35,
        child: GestureDetector(
          onTap: onTap,
          behavior: HitTestBehavior.opaque,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              SizedBox(
                width: 62,
                height: 62,
                child: Glass(
                  radius: 31,
                  tint: tint,
                  tintAlpha: danger || active ? 0.16 : 0.07,
                  glow: danger || active ? tint : null,
                  child: Center(
                    child: Icon(
                      icon,
                      size: 26,
                      color: danger
                          ? const Color(0xFFFF8FA3)
                          : active
                          ? tint
                          : Colors.white.withValues(alpha: 0.9),
                    ),
                  ),
                ),
              ),
              const SizedBox(height: 8),
              Text(
                label.toUpperCase(),
                style: TextStyle(
                  fontSize: 10,
                  letterSpacing: 2.2,
                  fontWeight: FontWeight.w600,
                  color: Colors.white.withValues(alpha: 0.55),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
