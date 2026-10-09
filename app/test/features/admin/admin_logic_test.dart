import 'package:flutter_test/flutter_test.dart';
import 'package:jarvis_office/features/admin/admin_common.dart';
import 'package:jarvis_office/features/admin/admin_page.dart';
import 'package:jarvis_office/features/admin/tabs/catalog_tab.dart';
import 'package:jarvis_office/features/admin/tabs/vendors_tab.dart';

import 'fake_admin_api.dart';

void main() {
  group('parseCents', () {
    const cases = <String, int?>{
      '0': 0,
      '35': 3500,
      '35.5': 3550,
      '35.50': 3550,
      '35.05': 3505,
      '1,250.99': 125099,
      r'S$12': 1200,
      r'$7.1': 710,
      ' 9.00 ': 900,
      '12.': 1200,
      '': null,
      'abc': null,
      '-5': null,
      '1.234': null,
      '1e3': null,
    };
    cases.forEach((input, want) {
      test('"$input" -> $want', () => expect(parseCents(input), want));
    });
  });

  test('centsToInput round-trips with parseCents', () {
    for (final c in [0, 5, 99, 100, 3550, 125099]) {
      expect(parseCents(centsToInput(c)), c);
    }
    expect(centsToInput(3505), '35.05');
  });

  group('Validators', () {
    final cases = <(String, String? Function(String?), String, bool)>[
      ('phone ok', Validators.phone, '+6562001001', true),
      ('phone spaces ok', Validators.phone, '+65 6200 1001', true),
      ('phone no plus', Validators.phone, '6562001001', false),
      ('phone too short', Validators.phone, '+65123', false),
      ('postal ok', Validators.sgPostal, '018936', true),
      ('postal 5 digits', Validators.sgPostal, '18936', false),
      ('postal letters', Validators.sgPostal, '01893a', false),
      ('email ok', Validators.email, 'a.b@x.sg', true),
      ('email bad', Validators.email, 'a.b@', false),
      ('qty ok', Validators.positiveInt, '3', true),
      ('qty zero', Validators.positiveInt, '0', false),
      ('money ok', Validators.money, '1.5', true),
      ('money bad', Validators.money, 'x', false),
    ];
    for (final (name, v, input, ok) in cases) {
      test(name, () => expect(v(input) == null, ok));
    }
  });

  group('filterCatalog', () {
    final items = FakeAdminApi().catalogItems;
    final cases = <(String, String, String?, List<String>)>[
      ('everything', '', null, ['c-a4', 'c-beans']),
      ('by sku', 'cof-beans', null, ['c-beans']),
      ('by alias', 'printer', null, ['c-a4']),
      ('by category text', 'stationery', null, ['c-a4']),
      ('category filter', '', 'Coffee & Tea', ['c-beans']),
      ('query + category mismatch', 'paper', 'Coffee & Tea', []),
    ];
    for (final (name, q, cat, want) in cases) {
      test(name, () {
        expect(
          filterCatalog(items, query: q, category: cat).map((i) => i.id),
          want,
        );
      });
    }
  });

  group('filterVendors', () {
    final vendors = FakeAdminApi().vendors;
    final cases = <(String, String, VendorFilter, List<String>)>[
      (
        'all sorted by priority',
        '',
        VendorFilter.all,
        ['v-popular', 'v-commonman', 'v-gjg'],
      ),
      ('allowed', '', VendorFilter.allowed, ['v-popular', 'v-commonman']),
      ('blocked', '', VendorFilter.blocked, ['v-gjg']),
      ('office', '', VendorFilter.office, ['v-popular', 'v-commonman']),
      ('by reap name', 'roasters', VendorFilter.all, ['v-commonman']),
      ('by domain', 'popular.com', VendorFilter.all, ['v-popular']),
    ];
    for (final (name, q, f, want) in cases) {
      test(name, () {
        expect(
          filterVendors(vendors, query: q, filter: f).map((v) => v.id),
          want,
        );
      });
    }
  });

  test('AdminTab slugs map to paths and fall back to catalog', () {
    expect(AdminTab.fromSlug('prompt'), AdminTab.prompt);
    expect(AdminTab.fromSlug('nope'), AdminTab.catalog);
    expect(AdminTab.fromSlug(null), AdminTab.catalog);
    expect(AdminTab.card.path, '/admin/card');
    expect(
      AdminTab.values.map((t) => t.slug).toSet().length,
      AdminTab.values.length,
    );
  });
}
