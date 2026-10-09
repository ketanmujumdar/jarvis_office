import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/providers.dart';
import '../features/voice/voice_controller.dart';
import 'auto_login.dart';
import 'context_cards.dart';
import 'glass.dart';
import 'orb.dart';

/// Root: signs Maya in, then shows the Jarvis voice screen.
class JarvisRoot extends ConsumerWidget {
  const JarvisRoot({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final login = ref.watch(autoLoginProvider);
    final signedIn = ref.watch(sessionProvider) != null;
    final Widget body;
    if (signedIn) {
      body = const JarvisScreen(key: ValueKey('jarvis'));
    } else if (login.hasError) {
      body = _Splash(
        key: const ValueKey('error'),
        message: describeLoginError(login.error!),
        onRetry: () {
          HapticFeedback.lightImpact();
          ref.invalidate(autoLoginProvider);
        },
      );
    } else {
      body = const _Splash(key: ValueKey('loading'));
    }
    return AnimatedSwitcher(
      duration: const Duration(milliseconds: 700),
      child: body,
    );
  }
}

/// Shared animated backdrop + orb clock for both screens.
mixin _OrbClock<T extends StatefulWidget> on State<T>, TickerProvider {
  late final OrbMotion motion = OrbMotion(initialStatus);
  late final AnimationController _clock;
  Duration _last = Duration.zero;

  VoiceStatus get initialStatus => VoiceStatus.idle;
  VoiceStatus get targetStatus;

  void startClock() {
    // A long repeating controller is the frame clock; phases integrate dt
    // so state changes alter speed without jumps.
    _clock =
        AnimationController(vsync: this, duration: const Duration(hours: 1))
          ..addListener(() {
            final now = _clock.lastElapsedDuration ?? Duration.zero;
            var dt = (now - _last).inMicroseconds / 1e6;
            _last = now;
            if (dt < 0 || dt > 0.1) dt = 1 / 60;
            motion.tick(dt, OrbLook.forStatus(targetStatus));
          })
          ..repeat();
  }

  void stopClock() {
    _clock.dispose();
    motion.dispose();
  }
}

class _Splash extends StatefulWidget {
  const _Splash({super.key, this.message, this.onRetry});
  final String? message;
  final VoidCallback? onRetry;

  @override
  State<_Splash> createState() => _SplashState();
}

class _SplashState extends State<_Splash>
    with SingleTickerProviderStateMixin, _OrbClock {
  @override
  VoiceStatus get targetStatus =>
      widget.message != null ? VoiceStatus.error : VoiceStatus.connecting;

  @override
  void initState() {
    super.initState();
    startClock();
  }

  @override
  void dispose() {
    stopClock();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final err = widget.message;
    return Scaffold(
      backgroundColor: JarvisColors.night,
      body: Stack(
        fit: StackFit.expand,
        children: [
          CustomPaint(painter: StarfieldPainter(motion)),
          SafeArea(
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 28),
              child: Column(
                children: [
                  const Spacer(flex: 3),
                  SizedBox.square(
                    dimension: 220,
                    child: CustomPaint(painter: OrbPainter(motion)),
                  ),
                  const SizedBox(height: 28),
                  _StatusLabel(
                    err == null ? 'INITIALISING JARVIS' : 'OFFLINE',
                    color: err == null ? JarvisColors.cyan : JarvisColors.red,
                  ),
                  const SizedBox(height: 14),
                  AnimatedOpacity(
                    duration: const Duration(milliseconds: 300),
                    opacity: err == null ? 0 : 1,
                    child: Text(
                      err ?? ' ',
                      textAlign: TextAlign.center,
                      style: TextStyle(
                        color: Colors.white.withValues(alpha: 0.7),
                        fontSize: 15,
                        height: 1.35,
                      ),
                    ),
                  ),
                  const Spacer(flex: 2),
                  if (widget.onRetry != null)
                    GestureDetector(
                      onTap: widget.onRetry,
                      child: const Glass(
                        radius: 28,
                        padding: EdgeInsets.symmetric(
                          horizontal: 34,
                          vertical: 16,
                        ),
                        child: Text(
                          'RETRY',
                          style: TextStyle(
                            color: Colors.white,
                            letterSpacing: 4,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      ),
                    ),
                  const SizedBox(height: 32),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// The single, full-screen voice experience.
class JarvisScreen extends ConsumerStatefulWidget {
  const JarvisScreen({super.key});

  @override
  ConsumerState<JarvisScreen> createState() => _JarvisScreenState();
}

class _JarvisScreenState extends ConsumerState<JarvisScreen>
    with SingleTickerProviderStateMixin, _OrbClock {
  @override
  VoiceStatus get targetStatus => ref.read(playgroundProvider).voiceStatus;

  String? _chip;
  Timer? _chipTimer;
  final _seenUpdates = <String>{};

  @override
  void initState() {
    super.initState();
    startClock();
  }

  @override
  void dispose() {
    _chipTimer?.cancel();
    stopClock();
    super.dispose();
  }

  void _onOrbTap() {
    final ctl = ref.read(playgroundProvider.notifier);
    final s = ref.read(playgroundProvider).voiceStatus;
    if (s == VoiceStatus.connecting) {
      HapticFeedback.selectionClick();
      return;
    }
    HapticFeedback.mediumImpact();
    if (s.isLive) {
      ctl.stopVoice();
    } else {
      ctl.startVoice();
    }
  }

  void _showUpdate(String text) {
    setState(() => _chip = text);
    HapticFeedback.lightImpact();
    _chipTimer?.cancel();
    _chipTimer = Timer(const Duration(seconds: 5), () {
      if (mounted) setState(() => _chip = null);
    });
  }

  @override
  Widget build(BuildContext context) {
    ref.listen<List<TranscriptEntry>>(
      playgroundProvider.select((s) => s.transcript),
      (_, list) {
        for (final e in list.reversed.take(4)) {
          if (e.speaker == Speaker.system &&
              e.text.startsWith('Update:') &&
              _seenUpdates.add(e.id)) {
            _showUpdate(e.text.substring('Update:'.length).trim());
            break;
          }
        }
      },
    );
    ref.listen<VoiceStatus>(playgroundProvider.select((s) => s.voiceStatus), (
      prev,
      next,
    ) {
      if (prev == VoiceStatus.connecting && next.isLive) {
        HapticFeedback.heavyImpact();
      } else if (next == VoiceStatus.error) {
        HapticFeedback.vibrate();
      }
    });

    final state = ref.watch(playgroundProvider);
    final user = ref.watch(currentUserProvider);
    final firstName = (user?.name ?? 'Maya').split(' ').first;
    final live = state.voiceStatus.isLive;
    final mq = MediaQuery.of(context);

    return Scaffold(
      backgroundColor: JarvisColors.night,
      body: Stack(
        fit: StackFit.expand,
        children: [
          CustomPaint(painter: StarfieldPainter(motion)),
          Padding(
            padding: EdgeInsets.only(
              top: mq.padding.top + 8,
              bottom: math.max(mq.padding.bottom, 12) + 4,
              left: 20,
              right: 20,
            ),
            child: Column(
              children: [
                _Greeting(name: firstName),
                Expanded(
                  child: LayoutBuilder(
                    builder: (context, box) {
                      final side = math.min(
                        box.maxWidth * 1.02,
                        box.maxHeight - 40,
                      );
                      return Column(
                        mainAxisAlignment: MainAxisAlignment.center,
                        children: [
                          Semantics(
                            button: true,
                            label: live ? 'Stop Jarvis' : 'Start Jarvis',
                            child: GestureDetector(
                              behavior: HitTestBehavior.opaque,
                              onTap: _onOrbTap,
                              child: SizedBox.square(
                                dimension: math.max(side, 120),
                                child: RepaintBoundary(
                                  child: CustomPaint(
                                    painter: OrbPainter(motion),
                                  ),
                                ),
                              ),
                            ),
                          ),
                          _StatusLabel(
                            _statusText(state),
                            color: _statusColor(state.voiceStatus),
                          ),
                        ],
                      );
                    },
                  ),
                ),
                const SizedBox(height: 10),
                _UpdateChip(text: _chip),
                SizedBox(
                  height: 118,
                  child: _Captions(
                    transcript: state.transcript,
                    error: state.voiceStatus == VoiceStatus.error
                        ? state.error
                        : null,
                  ),
                ),
                const SizedBox(height: 10),
                const ContextCardArea(),
                const SizedBox(height: 16),
                Row(
                  mainAxisAlignment: MainAxisAlignment.spaceEvenly,
                  children: [
                    GlassIconButton(
                      icon: state.muted
                          ? Icons.mic_off_rounded
                          : Icons.mic_none_rounded,
                      label: state.muted ? 'Muted' : 'Mute',
                      active: state.muted,
                      onTap: live
                          ? () {
                              HapticFeedback.lightImpact();
                              ref
                                  .read(playgroundProvider.notifier)
                                  .toggleMute();
                            }
                          : null,
                    ),
                    GlassIconButton(
                      icon: Icons.call_end_rounded,
                      label: 'End',
                      danger: live,
                      onTap: live || state.voiceStatus == VoiceStatus.connecting
                          ? () {
                              HapticFeedback.mediumImpact();
                              ref.read(playgroundProvider.notifier).stopVoice();
                            }
                          : null,
                    ),
                  ],
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  String _statusText(PlaygroundState s) {
    if (s.voiceStatus == VoiceStatus.idle) return 'TAP TO TALK';
    if (s.voiceStatus == VoiceStatus.error) return 'TAP TO RETRY';
    if (s.muted && s.voiceStatus.isLive) return 'MUTED';
    return s.voiceStatus.label.replaceAll('…', '').toUpperCase();
  }

  Color _statusColor(VoiceStatus s) => switch (s) {
    VoiceStatus.thinking => JarvisColors.magenta,
    VoiceStatus.error => JarvisColors.red,
    VoiceStatus.idle => Colors.white.withValues(alpha: 0.55),
    _ => JarvisColors.cyan,
  };
}

class _Greeting extends StatelessWidget {
  const _Greeting({required this.name});
  final String name;

  @override
  Widget build(BuildContext context) {
    final h = DateTime.now().hour;
    final part = h < 5
        ? 'evening'
        : h < 12
        ? 'morning'
        : h < 17
        ? 'afternoon'
        : 'evening';
    return Column(
      children: [
        Text(
          'J · A · R · V · I · S',
          style: TextStyle(
            fontSize: 10.5,
            letterSpacing: 3,
            fontWeight: FontWeight.w600,
            color: JarvisColors.cyan.withValues(alpha: 0.65),
          ),
        ),
        const SizedBox(height: 8),
        Text(
          'Good $part, $name',
          style: const TextStyle(
            color: Colors.white,
            fontSize: 26,
            fontWeight: FontWeight.w300,
            letterSpacing: 0.2,
          ),
        ),
      ],
    );
  }
}

class _StatusLabel extends StatelessWidget {
  const _StatusLabel(this.text, {required this.color});
  final String text;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return AnimatedSwitcher(
      duration: const Duration(milliseconds: 350),
      child: Text(
        text,
        key: ValueKey(text),
        style: TextStyle(
          color: color,
          fontSize: 13,
          letterSpacing: 5,
          fontWeight: FontWeight.w600,
          shadows: [
            Shadow(color: color.withValues(alpha: 0.6), blurRadius: 12),
          ],
        ),
      ),
    );
  }
}

/// Subtle chip for "Update:" system lines; fades out after a few seconds.
class _UpdateChip extends StatelessWidget {
  const _UpdateChip({required this.text});
  final String? text;

  @override
  Widget build(BuildContext context) {
    return AnimatedSize(
      duration: const Duration(milliseconds: 300),
      curve: Curves.easeOutCubic,
      child: AnimatedSwitcher(
        duration: const Duration(milliseconds: 400),
        child: text == null
            ? const SizedBox(width: double.infinity)
            : Padding(
                key: ValueKey(text),
                padding: const EdgeInsets.only(bottom: 8),
                child: Glass(
                  radius: 16,
                  tint: JarvisColors.violet,
                  tintAlpha: 0.12,
                  padding: const EdgeInsets.symmetric(
                    horizontal: 12,
                    vertical: 7,
                  ),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      const Icon(
                        Icons.bolt_rounded,
                        size: 14,
                        color: JarvisColors.magenta,
                      ),
                      const SizedBox(width: 6),
                      Flexible(
                        child: Text(
                          text!,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                            fontSize: 12,
                            color: Colors.white.withValues(alpha: 0.8),
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
      ),
    );
  }
}

/// Latest assistant line (bright) and latest user line (dim). Tool and
/// system entries are hidden.
class _Captions extends StatelessWidget {
  const _Captions({required this.transcript, this.error});
  final List<TranscriptEntry> transcript;
  final String? error;

  static String _tail(String s, int max) {
    final t = s.trim();
    if (t.length <= max) return t;
    final cut = t.substring(t.length - max);
    final space = cut.indexOf(' ');
    return '…${space > 0 && space < 24 ? cut.substring(space + 1) : cut}';
  }

  @override
  Widget build(BuildContext context) {
    TranscriptEntry? assistant;
    TranscriptEntry? user;
    for (var i = transcript.length - 1; i >= 0; i--) {
      final e = transcript[i];
      if (e.text.trim().isEmpty) continue;
      if (assistant == null && e.speaker == Speaker.assistant) {
        assistant = e;
      } else if (user == null && e.speaker == Speaker.user) {
        user = e;
      }
      if (assistant != null && user != null) break;
    }
    final showUser = user != null;
    final err = error;

    return Column(
      mainAxisAlignment: MainAxisAlignment.end,
      children: [
        AnimatedSwitcher(
          duration: const Duration(milliseconds: 400),
          child: showUser
              ? Padding(
                  key: ValueKey('u-${user.id}'),
                  padding: const EdgeInsets.only(bottom: 8),
                  child: Text(
                    '“${_tail(user.text, 90)}”',
                    maxLines: 2,
                    textAlign: TextAlign.center,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      color: Colors.white.withValues(alpha: 0.42),
                      fontSize: 14,
                      fontStyle: FontStyle.italic,
                      height: 1.3,
                    ),
                  ),
                )
              : const SizedBox.shrink(key: ValueKey('u-none')),
        ),
        AnimatedSwitcher(
          duration: const Duration(milliseconds: 450),
          transitionBuilder: (child, anim) => FadeTransition(
            opacity: anim,
            child: SlideTransition(
              position: Tween(
                begin: const Offset(0, 0.15),
                end: Offset.zero,
              ).animate(anim),
              child: child,
            ),
          ),
          child: err != null
              ? Text(
                  err,
                  key: ValueKey('err-$err'),
                  maxLines: 3,
                  textAlign: TextAlign.center,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    color: Color(0xFFFF8FA3),
                    fontSize: 15,
                  ),
                )
              : assistant == null
              ? const SizedBox.shrink(key: ValueKey('a-none'))
              : Text(
                  _tail(assistant.text, 150),
                  key: ValueKey('a-${assistant.id}'),
                  maxLines: 3,
                  textAlign: TextAlign.center,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                    color: Colors.white,
                    fontSize: 18,
                    fontWeight: FontWeight.w400,
                    height: 1.35,
                  ),
                ),
        ),
      ],
    );
  }
}
