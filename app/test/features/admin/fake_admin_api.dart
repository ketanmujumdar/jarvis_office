import 'package:jarvis_office/core/api/api_client.dart';
import 'package:jarvis_office/core/api/models.dart';
import 'package:jarvis_office/features/admin/url_opener.dart';

/// In-memory fake of the admin + auth endpoints. Records every mutation.
class FakeAdminApi extends JarvisApi {
  FakeAdminApi() : super(baseUrl: 'http://fake.local');

  final calls = <String>[];
  final bodies = <String, Json>{};

  Object? failWith;
  bool defaultPromptMissing = false;
  bool usersFail = false;

  List<User> users = const [
    User(
      id: 'u-maya',
      name: 'Maya Tan',
      email: 'maya.tan@example.com',
      role: Role.manager,
    ),
    User(
      id: 'u-daniel',
      name: 'Daniel Lim',
      email: 'daniel.lim@example.com',
      role: Role.approver,
    ),
    User(
      id: 'u-priya',
      name: 'Priya Nair',
      email: 'priya.nair@example.com',
      role: Role.admin,
    ),
  ];

  List<Vendor> vendors = [
    const Vendor(
      id: 'v-popular',
      domain: 'popular.com.sg',
      name: 'Popular Bookstore',
      reapMerchantName: 'Popular Online',
      category: 'Stationery',
      country: 'SG',
      allowed: true,
      officeRelevant: true,
      priority: 10,
    ),
    const Vendor(
      id: 'v-commonman',
      domain: 'commonmancoffeeroasters.com',
      name: 'Common Man Coffee',
      reapMerchantName: 'Common Man Coffee Roasters SG',
      category: 'Coffee',
      country: 'SG',
      allowed: true,
      officeRelevant: true,
      priority: 20,
    ),
    const Vendor(
      id: 'v-gjg',
      domain: 'greatjonesgoods.com',
      name: 'Great Jones',
      reapMerchantName: '',
      category: 'Kitchen',
      country: 'SG',
      allowed: false,
      officeRelevant: false,
      priority: 90,
    ),
  ];

  List<CatalogItem> catalogItems = [
    const CatalogItem(
      id: 'c-a4',
      sku: 'PAP-A4-80',
      name: 'A4 copy paper 80gsm',
      aliases: ['printer paper'],
      category: 'Paper & Stationery',
      unit: 'ream',
      defaultQty: 5,
      maxUnitPriceCents: 900,
      preferredVendorIds: ['v-popular'],
      autoApprove: true,
      searchQuery: 'A4 paper 80gsm',
      active: true,
    ),
    const CatalogItem(
      id: 'c-beans',
      sku: 'COF-BEANS-1KG',
      name: 'Espresso beans 1kg',
      aliases: ['the usual coffee'],
      category: 'Coffee & Tea',
      unit: 'bag',
      defaultQty: 2,
      maxUnitPriceCents: 6000,
      preferredVendorIds: ['v-commonman'],
      autoApprove: false,
      searchQuery: 'espresso beans 1kg',
      active: true,
    ),
  ];

  PolicyConfig policy = PolicyConfig(
    currency: 'SGD',
    perOrderLimitCents: 50000,
    monthlyBudgetCents: 300000,
    priceDriftPct: 5,
    updatedAt: DateTime.utc(2026, 10, 1),
  );

  List<Address> addressList = [
    const Address(
      id: 'a-one-north',
      label: 'R&D Studio - one-north',
      firstName: 'Arjun',
      lastName: 'Menon',
      phone: '+6562001002',
      email: 'arjun.menon@example.com',
      addressLine1: '1 Fusionopolis Way',
      addressLine2: '#10-05 Connexis North Tower',
      city: 'Singapore',
      postalCode: '138632',
      country: 'SG',
      isDefault: false,
    ),
    const Address(
      id: 'a-hq',
      label: 'HQ - Marina One',
      firstName: 'Maya',
      lastName: 'Tan',
      phone: '+6562001001',
      email: 'maya.tan@example.com',
      addressLine1: '7 Straits View',
      addressLine2: '#20-01 Marina One East Tower',
      city: 'Singapore',
      postalCode: '018936',
      country: 'SG',
      isDefault: true,
    ),
  ];

  SystemPrompt prompt = SystemPrompt(
    key: 'agent',
    content: 'You are Jarvis.',
    version: 3,
    updatedAt: DateTime.utc(2026, 10, 9, 8),
    updatedBy: 'Priya Nair',
  );
  String defaultPrompt = 'You are Jarvis, the default prompt.';

  Enrollment? enrollment;

  Future<T> _do<T>(String call, T Function() f, [Json? body]) async {
    calls.add(call);
    if (body != null) bodies[call] = body;
    final err = failWith;
    if (err != null) throw err;
    return f();
  }

  // ---- auth
  @override
  Future<List<User>> demoUsers() => _do('demoUsers', () {
    if (usersFail) {
      throw const ApiException(statusCode: 0, code: 'network', message: 'down');
    }
    return users;
  });

  @override
  Future<LoginResponse> login(String email) => _do('login', () {
    final u = users.where((u) => u.email == email).firstOrNull;
    if (u == null) {
      throw const ApiException(
        statusCode: 404,
        code: 'not_found',
        message: 'no such user',
      );
    }
    return LoginResponse(token: u.id, user: u);
  }, {'email': email});

  // ---- catalog
  @override
  Future<List<CatalogItem>> adminCatalog() =>
      _do('adminCatalog', () => List.of(catalogItems));

  CatalogItem _item(String id, Json j) =>
      CatalogItem.fromJson({'id': id, ...j});

  @override
  Future<CatalogItem> createCatalogItem(Json input) =>
      _do('createCatalogItem', () {
        final item = _item('c-new-${catalogItems.length}', input);
        catalogItems = [...catalogItems, item];
        return item;
      }, input);

  @override
  Future<CatalogItem> updateCatalogItem(String id, Json input) =>
      _do('updateCatalogItem', () {
        final item = _item(id, input);
        catalogItems = [for (final i in catalogItems) i.id == id ? item : i];
        return item;
      }, input);

  @override
  Future<void> deleteCatalogItem(String id) => _do('deleteCatalogItem', () {
    catalogItems = catalogItems.where((i) => i.id != id).toList();
  });

  // ---- vendors
  @override
  Future<List<Vendor>> adminVendors() =>
      _do('adminVendors', () => List.of(vendors));

  @override
  Future<Vendor> updateVendor(String id, Json input) => _do('updateVendor', () {
    final v = Vendor.fromJson({'id': id, ...input});
    vendors = [for (final x in vendors) x.id == id ? v : x];
    return v;
  }, input);

  // ---- policy
  @override
  Future<PolicyConfig> getPolicy() => _do('getPolicy', () => policy);

  @override
  Future<PolicyConfig> updatePolicy(Json input) => _do('updatePolicy', () {
    policy = PolicyConfig.fromJson({
      'currency': 'SGD',
      ...input,
      'updated_at': DateTime.utc(2026, 10, 9, 9).toIso8601String(),
    });
    return policy;
  }, input);

  @override
  Future<SpendSummary> monthlySpend({int months = 6}) => _do(
    'monthlySpend',
    () => const SpendSummary(
      currency: 'SGD',
      monthlyBudgetCents: 300000,
      monthToDateCents: 120000,
      months: [],
    ),
  );

  // ---- addresses
  @override
  Future<List<Address>> adminAddresses() =>
      _do('adminAddresses', () => List.of(addressList));

  List<Address> _withDefault(Address a) => [
    for (final x in addressList)
      if (x.id != a.id)
        a.isDefault
            ? Address.fromJson({
                'id': x.id,
                ...x.toInput(),
                'is_default': false,
              })
            : x,
  ];

  @override
  Future<Address> createAddress(Json input) => _do('createAddress', () {
    final a = Address.fromJson({'id': 'a-new', ...input});
    addressList = [..._withDefault(a), a];
    return a;
  }, input);

  @override
  Future<Address> updateAddress(String id, Json input) =>
      _do('updateAddress', () {
        final a = Address.fromJson({'id': id, ...input});
        addressList = [..._withDefault(a), a];
        return a;
      }, input);

  @override
  Future<void> deleteAddress(String id) => _do('deleteAddress', () {
    addressList = addressList.where((a) => a.id != id).toList();
  });

  // ---- system prompt
  @override
  Future<SystemPrompt> getSystemPrompt() =>
      _do('getSystemPrompt', () => prompt);

  @override
  Future<SystemPrompt> updateSystemPrompt(String content) =>
      _do('updateSystemPrompt', () {
        prompt = SystemPrompt(
          key: 'agent',
          content: content,
          version: prompt.version + 1,
          updatedAt: DateTime.now().toUtc(),
          updatedBy: 'Priya Nair',
        );
        return prompt;
      }, {'content': content});

  @override
  Future<String> defaultSystemPrompt() => _do('defaultSystemPrompt', () {
    if (defaultPromptMissing) {
      throw const ApiException(
        statusCode: 501,
        code: 'not_implemented',
        message: 'not implemented',
      );
    }
    return defaultPrompt;
  });

  // ---- enrollment
  @override
  Future<Enrollment?> currentEnrollment() =>
      _do('currentEnrollment', () => enrollment);

  @override
  Future<Enrollment> startEnrollment() => _do('startEnrollment', () {
    enrollment = Enrollment(
      id: 'e1',
      reapEnrollmentId: 'enr_123',
      status: EnrollmentStatus.requiresAction,
      createdAt: DateTime.utc(2026, 10, 9, 9),
      nextActionUrl: 'https://sg.sandbox.reap.example/enroll/enr_123',
      ownerEmail: 'priya.nair@example.com',
    );
    return enrollment!;
  });
}

/// Records opened URLs instead of opening a browser.
class RecordingUrlOpener extends UrlOpener {
  RecordingUrlOpener({this.result = true});
  final bool result;
  final opened = <String>[];

  @override
  Future<bool> open(String url) async {
    opened.add(url);
    return result;
  }
}
