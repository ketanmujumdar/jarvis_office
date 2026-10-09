import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:jarvis_office/core/api/api_client.dart';
import 'package:jarvis_office/core/api/models.dart';
import 'package:jarvis_office/core/providers.dart';
import 'package:jarvis_office/core/theme/app_theme.dart';
import 'package:jarvis_office/features/admin/admin_page.dart';
import 'package:jarvis_office/features/admin/admin_providers.dart';

import '../../helpers.dart';
import 'fake_admin_api.dart';

Future<void> pumpAdmin(
  WidgetTester tester,
  FakeAdminApi api, {
  AdminTab tab = AdminTab.catalog,
  RecordingUrlOpener? opener,
  Size size = const Size(1400, 2200),
}) async {
  setSurface(tester, size);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        apiProvider.overrideWithValue(api),
        urlOpenerProvider.overrideWithValue(opener ?? RecordingUrlOpener()),
      ],
      child: themed(Scaffold(body: AdminPage(tab: tab))),
    ),
  );
  await tester.pumpAndSettle();
}

Future<void> enter(WidgetTester tester, String key, String text) async {
  final f = find.descendant(
    of: find.byKey(ValueKey(key)),
    matching: find.byType(EditableText),
  );
  await tester.enterText(
    f.evaluate().isEmpty ? find.byKey(ValueKey(key)) : f,
    text,
  );
  await tester.pump();
}

void main() {
  setUp(() => AppTheme.useGoogleFonts = false);

  group('tabs', () {
    testWidgets('shows every section and switches without a router', (
      tester,
    ) async {
      final api = FakeAdminApi();
      await pumpAdmin(tester, api);
      for (final t in AdminTab.values) {
        expect(find.text(t.label), findsOneWidget);
      }
      await tester.tap(find.byKey(const ValueKey('admin-tab-vendors')));
      await tester.pumpAndSettle();
      expect(find.text('ALLOWED VENDORS'), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey('admin-tab-prompt')));
      await tester.pumpAndSettle();
      expect(find.text('Version 3'), findsOneWidget);
    });

    testWidgets('renders in dark mode and on a phone without overflow', (
      tester,
    ) async {
      final api = FakeAdminApi();
      setSurface(tester, const Size(390, 2400));
      for (final tab in AdminTab.values) {
        await tester.pumpWidget(
          ProviderScope(
            key: ValueKey(tab),
            overrides: [
              apiProvider.overrideWithValue(api),
              urlOpenerProvider.overrideWithValue(RecordingUrlOpener()),
            ],
            child: themed(
              Scaffold(body: AdminPage(tab: tab)),
              brightness: Brightness.dark,
            ),
          ),
        );
        await tester.pumpAndSettle();
        expect(tester.takeException(), isNull, reason: tab.name);
      }
    });

    testWidgets('on a phone every section tab is on screen', (tester) async {
      final api = FakeAdminApi();
      await pumpAdmin(tester, api, size: const Size(390, 2400));
      for (final t in AdminTab.values) {
        final r = tester.getRect(find.byKey(ValueKey('admin-tab-${t.slug}')));
        expect(r.right, lessThanOrEqualTo(390), reason: t.slug);
      }
      await tester.tap(find.byKey(const ValueKey('admin-tab-card')));
      await tester.pumpAndSettle();
      expect(tester.takeException(), isNull);
    });

    testWidgets('load errors show a retry state', (tester) async {
      final api = FakeAdminApi()
        ..failWith = const ApiException(
          statusCode: 500,
          code: 'internal',
          message: 'database down',
        );
      await pumpAdmin(tester, api);
      expect(find.text('Something went wrong'), findsOneWidget);
      expect(find.text('database down'), findsOneWidget);
      api.failWith = null;
      await tester.tap(find.text('Try again'));
      await tester.pumpAndSettle();
      expect(find.text('A4 copy paper 80gsm'), findsOneWidget);
    });
  });

  group('catalog', () {
    testWidgets('lists items and filters by search and category', (
      tester,
    ) async {
      final api = FakeAdminApi();
      await pumpAdmin(tester, api);
      expect(find.text('A4 copy paper 80gsm'), findsOneWidget);
      expect(find.text('Espresso beans 1kg'), findsOneWidget);
      expect(find.text('2 items'), findsOneWidget);
      expect(find.text('Needs approval'), findsOneWidget);

      await tester.enterText(find.byType(TextField).first, 'usual coffee');
      await tester.pump();
      expect(find.text('A4 copy paper 80gsm'), findsNothing);
      expect(find.text('Espresso beans 1kg'), findsOneWidget);

      await tester.enterText(find.byType(TextField).first, '');
      await tester.tap(find.widgetWithText(ChoiceChip, 'Paper & Stationery'));
      await tester.pump();
      expect(find.text('A4 copy paper 80gsm'), findsOneWidget);
      expect(find.text('Espresso beans 1kg'), findsNothing);

      await tester.enterText(find.byType(TextField).first, 'zzz');
      await tester.pump();
      expect(find.text('No matching items'), findsOneWidget);
    });

    testWidgets('edit dialog validates and saves the item', (tester) async {
      final api = FakeAdminApi();
      await pumpAdmin(tester, api);
      await tester.tap(find.byKey(const ValueKey('catalog-row-c-a4')));
      await tester.pumpAndSettle();
      expect(find.text('Edit catalog item'), findsOneWidget);
      expect(find.text('Popular Bookstore'), findsWidgets);

      await enter(tester, 'field-max-price', 'abc');
      await tester.tap(find.text('Save changes'));
      await tester.pumpAndSettle();
      expect(find.text('Enter an amount like 35.50'), findsOneWidget);
      expect(api.calls, isNot(contains('updateCatalogItem')));

      await enter(tester, 'field-max-price', '12.5');
      await enter(tester, 'field-name', 'A4 paper (premium)');
      await tester.tap(find.text('Save changes'));
      await tester.pumpAndSettle();

      final body = api.bodies['updateCatalogItem']!;
      expect(body['name'], 'A4 paper (premium)');
      expect(body['max_unit_price_cents'], 1250);
      expect(body['preferred_vendor_ids'], ['v-popular']);
      expect(find.text('Edit catalog item'), findsNothing);
      expect(find.text('A4 paper (premium)'), findsOneWidget);
      expect(find.text('Item updated'), findsOneWidget);
    });

    testWidgets('new item requires a preferred vendor', (tester) async {
      final api = FakeAdminApi();
      await pumpAdmin(tester, api);
      await tester.tap(find.text('New item'));
      await tester.pumpAndSettle();
      await enter(tester, 'field-name', 'Green tea bags');
      await enter(tester, 'field-sku', 'TEA-GREEN-100');
      await enter(tester, 'field-category', 'Coffee & Tea');
      await enter(tester, 'field-max-price', '8');
      await tester.tap(find.text('Add item'));
      await tester.pumpAndSettle();
      expect(find.text('Pick at least one preferred vendor'), findsOneWidget);
      expect(api.calls, isNot(contains('createCatalogItem')));

      await tester.tap(find.text('Add vendor'));
      await tester.pumpAndSettle();
      // Blocked vendors are not offered.
      expect(find.text('Great Jones'), findsNothing);
      await tester.tap(find.text('Common Man Coffee').last);
      await tester.pumpAndSettle();
      await tester.tap(find.text('Add item'));
      await tester.pumpAndSettle();
      final body = api.bodies['createCatalogItem']!;
      expect(body['preferred_vendor_ids'], ['v-commonman']);
      expect(body['max_unit_price_cents'], 800);
      expect(body['default_qty'], 1);
      expect(find.text('Green tea bags'), findsOneWidget);
    });

    testWidgets('delete asks for confirmation', (tester) async {
      final api = FakeAdminApi();
      await pumpAdmin(tester, api);
      await tester.tap(find.byKey(const ValueKey('catalog-row-c-beans')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Delete'));
      await tester.pumpAndSettle();
      expect(find.text('Delete Espresso beans 1kg?'), findsOneWidget);
      await tester.tap(find.widgetWithText(FilledButton, 'Delete'));
      await tester.pumpAndSettle();
      expect(api.calls, contains('deleteCatalogItem'));
      expect(find.text('Espresso beans 1kg'), findsNothing);
    });
  });

  group('vendors', () {
    testWidgets('allowed toggle and priority stepper update the vendor', (
      tester,
    ) async {
      final api = FakeAdminApi();
      await pumpAdmin(tester, api, tab: AdminTab.vendors);
      expect(find.text('2 / 3'), findsOneWidget);
      expect(find.textContaining('not resolved on Reap'), findsOneWidget);

      await tester.tap(find.byKey(const ValueKey('allowed-v-popular')));
      await tester.pumpAndSettle();
      expect(api.bodies['updateVendor']!['allowed'], false);
      expect(api.bodies['updateVendor']!['domain'], 'popular.com.sg');
      expect(find.text('1 / 3'), findsOneWidget);
      expect(find.text('Popular Bookstore blocked'), findsOneWidget);

      await tester.tap(
        find.descendant(
          of: find.byKey(const ValueKey('priority-v-commonman')),
          matching: find.byIcon(Icons.add_rounded),
        ),
      );
      await tester.pumpAndSettle();
      expect(api.bodies['updateVendor']!['priority'], 25);
      expect(api.vendors.firstWhere((v) => v.id == 'v-commonman').priority, 25);
    });

    testWidgets('filters blocked vendors', (tester) async {
      final api = FakeAdminApi();
      await pumpAdmin(tester, api, tab: AdminTab.vendors);
      await tester.tap(find.widgetWithText(ChoiceChip, 'Blocked'));
      await tester.pump();
      expect(find.text('Great Jones'), findsOneWidget);
      expect(find.text('Popular Bookstore'), findsNothing);
    });
  });

  group('policy', () {
    testWidgets('saves limits as cents and validates', (tester) async {
      final api = FakeAdminApi();
      await pumpAdmin(tester, api, tab: AdminTab.policy);
      expect(find.text('500.00'), findsOneWidget);
      expect(find.text('3000.00'), findsOneWidget);
      // Save is disabled until something changes.
      final save = find.widgetWithText(FilledButton, 'Save limits');
      expect(tester.widget<FilledButton>(save).onPressed, isNull);

      Finder field(String key) => find.descendant(
        of: find.byKey(ValueKey(key)),
        matching: find.byType(TextFormField),
      );
      await tester.enterText(field('policy-monthly'), '100');
      await tester.pump();
      await tester.tap(save);
      await tester.pumpAndSettle();
      expect(
        find.text('Monthly budget should be at least the per-order limit'),
        findsOneWidget,
      );

      await tester.enterText(field('policy-monthly'), '4,000');
      await tester.enterText(field('policy-drift'), '7.5');
      await tester.enterText(field('policy-per-order'), '750.25');
      await tester.pump();
      await tester.tap(save);
      await tester.pumpAndSettle();
      expect(api.bodies['updatePolicy'], {
        'per_order_limit_cents': 75025,
        'monthly_budget_cents': 400000,
        'price_drift_pct': 7.5,
      });
      expect(find.text('Policy limits saved'), findsOneWidget);
      // Budget meter uses month-to-date spend.
      expect(find.textContaining('of the monthly budget used'), findsOneWidget);
    });
  });

  group('addresses', () {
    testWidgets('lists default first with a chip', (tester) async {
      final api = FakeAdminApi();
      await pumpAdmin(tester, api, tab: AdminTab.addresses);
      final hq = tester.getTopLeft(find.text('HQ - Marina One'));
      final rd = tester.getTopLeft(find.text('R&D Studio - one-north'));
      expect(hq.dy < rd.dy || hq.dx < rd.dx, isTrue);
      expect(find.text('Default'), findsOneWidget);
      expect(
        find.text(
          '7 Straits View, #20-01 Marina One East Tower, Singapore 018936',
        ),
        findsOneWidget,
      );
    });

    testWidgets('add address validates Singapore fields', (tester) async {
      final api = FakeAdminApi();
      await pumpAdmin(tester, api, tab: AdminTab.addresses);
      await tester.tap(find.widgetWithText(FilledButton, 'Add address'));
      await tester.pumpAndSettle();
      await enter(tester, 'addr-label', 'Warehouse - Tuas');
      await enter(tester, 'addr-line1', '10 Tuas South Ave 2');
      await enter(tester, 'addr-postal', '6375');
      await enter(tester, 'addr-first', 'Wei');
      await enter(tester, 'addr-last', 'Ng');
      await enter(tester, 'addr-phone', '6562001003');
      await enter(tester, 'addr-email', 'wei.ng@example.com');
      await tester.tap(find.widgetWithText(FilledButton, 'Add address').last);
      await tester.pumpAndSettle();
      expect(find.text('Singapore postal codes have 6 digits'), findsOneWidget);
      expect(
        find.text('Use international format, e.g. +6562001001'),
        findsOneWidget,
      );

      await enter(tester, 'addr-postal', '637535');
      await enter(tester, 'addr-phone', '+65 6200 1003');
      await tester.tap(find.widgetWithText(FilledButton, 'Add address').last);
      await tester.pumpAndSettle();
      final body = api.bodies['createAddress']!;
      expect(body['phone'], '+6562001003');
      expect(body['country'], 'SG');
      expect(body['city'], 'Singapore');
      expect(body['postal_code'], '637535');
      expect(find.text('Warehouse - Tuas'), findsOneWidget);
    });

    testWidgets('set as default and delete', (tester) async {
      final api = FakeAdminApi();
      await pumpAdmin(tester, api, tab: AdminTab.addresses);
      await tester.tap(find.widgetWithText(TextButton, 'Set as default'));
      await tester.pumpAndSettle();
      expect(api.bodies['updateAddress']!['is_default'], true);
      expect(
        api.addressList.firstWhere((a) => a.id == 'a-one-north').isDefault,
        isTrue,
      );
      expect(
        api.addressList.firstWhere((a) => a.id == 'a-hq').isDefault,
        isFalse,
      );

      await tester.tap(find.byTooltip('Actions for HQ - Marina One'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Delete').last);
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(FilledButton, 'Delete'));
      await tester.pumpAndSettle();
      expect(api.calls, contains('deleteAddress'));
      expect(find.text('HQ - Marina One'), findsNothing);
    });
  });

  group('system prompt', () {
    testWidgets('edit, save bumps the version', (tester) async {
      final api = FakeAdminApi();
      await pumpAdmin(tester, api, tab: AdminTab.prompt);
      expect(find.text('Version 3'), findsOneWidget);
      expect(find.textContaining('by Priya Nair'), findsOneWidget);
      final save = find.widgetWithText(FilledButton, 'Save prompt');
      expect(tester.widget<FilledButton>(save).onPressed, isNull);

      await tester.enterText(
        find.byKey(const ValueKey('prompt-editor')),
        'You are Jarvis. Always ask which address.',
      );
      await tester.pump();
      expect(find.text('Unsaved changes'), findsOneWidget);
      await tester.tap(save);
      await tester.pumpAndSettle();
      expect(api.bodies['updateSystemPrompt'], {
        'content': 'You are Jarvis. Always ask which address.',
      });
      expect(find.text('Version 4'), findsOneWidget);
      expect(find.text('Unsaved changes'), findsNothing);
    });

    testWidgets('empty prompt cannot be saved; discard restores', (
      tester,
    ) async {
      final api = FakeAdminApi();
      await pumpAdmin(tester, api, tab: AdminTab.prompt);
      await tester.enterText(find.byKey(const ValueKey('prompt-editor')), '  ');
      await tester.pump();
      final save = find.widgetWithText(FilledButton, 'Save prompt');
      expect(tester.widget<FilledButton>(save).onPressed, isNull);
      await tester.tap(find.text('Discard'));
      await tester.pump();
      expect(find.text('You are Jarvis.'), findsOneWidget);
    });

    testWidgets('reset to default loads the default text for review', (
      tester,
    ) async {
      final api = FakeAdminApi();
      await pumpAdmin(tester, api, tab: AdminTab.prompt);
      await tester.tap(find.text('Reset to default'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Load default'));
      await tester.pumpAndSettle();
      expect(find.text('You are Jarvis, the default prompt.'), findsOneWidget);
      expect(
        find.text('Default prompt loaded. Save to apply it.'),
        findsOneWidget,
      );
      expect(api.calls, isNot(contains('updateSystemPrompt')));
      await tester.tap(find.widgetWithText(FilledButton, 'Save prompt'));
      await tester.pumpAndSettle();
      expect(api.prompt.content, 'You are Jarvis, the default prompt.');
    });

    testWidgets('reset reports a server without the default endpoint', (
      tester,
    ) async {
      final api = FakeAdminApi()..defaultPromptMissing = true;
      await pumpAdmin(tester, api, tab: AdminTab.prompt);
      await tester.tap(find.text('Reset to default'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Load default'));
      await tester.pumpAndSettle();
      expect(
        find.text('The server does not support this yet.'),
        findsOneWidget,
      );
      expect(find.text('You are Jarvis.'), findsOneWidget);
    });
  });

  group('card enrollment', () {
    testWidgets('starts hosted enrollment and opens the Reap page', (
      tester,
    ) async {
      final api = FakeAdminApi();
      final opener = RecordingUrlOpener();
      await pumpAdmin(tester, api, tab: AdminTab.card, opener: opener);
      expect(find.text('No card'), findsOneWidget);
      await tester.tap(find.text('Set up card'));
      await tester.pumpAndSettle();
      expect(api.calls, contains('startEnrollment'));
      expect(opener.opened, ['https://sg.sandbox.reap.example/enroll/enr_123']);
      expect(find.text('Card setup pending'), findsOneWidget);
      expect(find.text('enr_123'), findsOneWidget);
      expect(find.text('Continue on Reap'), findsOneWidget);
    });

    testWidgets('shows the link when the browser cannot be opened', (
      tester,
    ) async {
      final api = FakeAdminApi();
      await pumpAdmin(
        tester,
        api,
        tab: AdminTab.card,
        opener: RecordingUrlOpener(result: false),
      );
      await tester.tap(find.text('Set up card'));
      await tester.pumpAndSettle();
      expect(
        find.text('https://sg.sandbox.reap.example/enroll/enr_123'),
        findsOneWidget,
      );
    });

    testWidgets('active card offers replace and check status', (tester) async {
      final api = FakeAdminApi()
        ..enrollment = Enrollment(
          id: 'e1',
          reapEnrollmentId: 'enr_9',
          status: EnrollmentStatus.active,
          createdAt: DateTime.utc(2026, 10, 1),
        );
      await pumpAdmin(tester, api, tab: AdminTab.card);
      expect(find.text('Card ready'), findsOneWidget);
      expect(find.text('Replace card'), findsOneWidget);
      await tester.tap(find.text('Check status'));
      await tester.pumpAndSettle();
      expect(find.text('Card is ready for checkout.'), findsOneWidget);
      // Never shows card digits: the model has none.
      expect(find.textContaining('last4'), findsNothing);
    });
  });
}
