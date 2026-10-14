'use strict';

(() => {
  const origins = {willys: 'https://www.willys.se', hemkop: 'https://www.hemkop.se', mathem: 'https://www.mathem.se'};
  const names = {willys: 'Willys', hemkop: 'Hemköp', mathem: 'Mathem'};
  const number = (value) => Number(String(value).replaceAll('\u00a0', '').replace(',', '.'));
  const money = (value) => {
    const amount = number(value);
    if (!Number.isFinite(amount) || amount < 0 || amount > 1000000) throw new Error('Invalid retailer price.');
    return {amount_minor: Math.round(amount * 100), currency: 'SEK'};
  };
  function pack(text) {
    const match = String(text).toLowerCase().match(/(?:([0-9]+)\s*[x×]\s*)?([0-9]+(?:[.,][0-9]+)?)\s*(kg|g|ml|cl|dl|l|st|stycken|pcs|pack|p)(?:\b|$)/);
    if (!match) return null;
    const units = {kg: ['g', 1000], g: ['g', 1], l: ['ml', 1000], dl: ['ml', 100], cl: ['ml', 10], ml: ['ml', 1], st: ['each', 1], stycken: ['each', 1], pcs: ['each', 1], pack: ['each', 1], p: ['each', 1]};
    const [unit, factor] = units[match[3]];
    const quantity = Math.round(Number(match[1] || 1) * number(match[2]) * factor);
    return quantity > 0 && quantity <= 1000000000 ? {quantity, unit} : null;
  }
  function traits(labels) {
    const values = new Set(labels.map((label) => String(label).toLowerCase()));
    const aliases = {organic: ['ecological', 'krav', 'eu_ecological', 'organic', 'ekologiskt', 'ekologisk'], vegan: ['vegan', 'veganskt'], lactose_free: ['lactose_free', 'lactose free', 'laktosfri', 'laktosfritt'], gluten_free: ['gluten_free', 'gluten free', 'glutenfri', 'glutenfritt']};
    return Object.fromEntries(Object.entries(aliases).map(([trait, words]) => [trait, words.some((word) => values.has(word)) ? 'yes' : 'unknown']));
  }
  function axfoodProduct(raw) {
    if (raw.tobaccoProduct || raw.tobaccoFreeNicotineProduct || raw.nicotineMedicalProduct || raw.isDrugProduct) return null;
    const byWeight = raw.productBasketType?.code === 'KG';
    const size = byWeight ? {quantity: 1000, unit: 'g'} : pack(raw.displayVolume);
    if (!size || !raw.code || !raw.name || raw.priceValue == null) return null;
    const weightPrice = /\/kg\b/.test(raw.priceUnit || '');
    let value = number(raw.priceValue);
    if (weightPrice && !byWeight) {
      if (!(number(raw.averageWeight) > 0)) return null;
      value *= number(raw.averageWeight);
    }
    return {id: raw.code, name: raw.name, brand: raw.manufacturer || '', pack: size, price: money(value), price_kind: weightPrice || byWeight ? 'estimated' : 'fixed', availability: raw.outOfStock || raw.addToCartDisabled ? 'out_of_stock' : raw.online === true ? 'in_stock' : 'unknown', traits: traits(raw.labels || [])};
  }
  function mathemProduct(raw) {
    const size = pack(raw.name_extra);
    if (!size || !raw.id || !raw.full_name || raw.gross_price == null || raw.currency !== 'SEK') return null;
    return {id: String(raw.id), name: raw.full_name, brand: raw.brand || '', pack: size, price: money(raw.gross_price), price_kind: /(?:ca\.?|cirka|approx)/i.test(raw.name_extra) ? 'estimated' : 'fixed', availability: raw.availability?.is_available === true ? 'in_stock' : raw.availability?.is_available === false ? 'out_of_stock' : 'unknown', traits: traits((raw.client_classifiers || []).map((entry) => entry.name))};
  }
  const sameContext = (a, b) => ['store_id', 'postal_code', 'fulfillment', 'delivery_slot_id', 'session_id'].every((key) => (a[key] || '') === (b[key] || '')) && JSON.stringify(a.memberships || []) === JSON.stringify(b.memberships || []);
  const hash = async (value) => Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(JSON.stringify(value))))).map((byte) => byte.toString(16).padStart(2, '0')).join('');
  const postal = (value) => String(value || '').replaceAll(' ', '').match(/^\d{5}$/)?.[0] || '00000';
  function selection(value) {
    if (Array.isArray(value)) return value.map(selection);
    if (!value || typeof value !== 'object') return value;
    return Object.fromEntries(Object.entries(value).filter(([key]) => !/(token|timestamp|expires|last_login|last_seen|nonce)/i.test(key)).map(([key, child]) => [key, selection(child)]));
  }
  function create(id, transport) {
    if (!origins[id]) throw new Error('Unsupported retailer.');
    const request = transport.request;
    const digest = transport.digest || hash;
    const base = id === 'willys' ? '/axfood/rest/v2' : '/axfood/rest';
    const getCart = () => request(id === 'mathem' ? '/api/v1/cart/?group-by=none' : `${base}/cart`);
    async function session() {
      if (id === 'mathem') {
        const data = await request('/api/v1/user/session/');
        const scope = selection(data);
        const context = {store_id: 'mathem', postal_code: postal(data?.delivery_address?.postal_code || data?.postal_code), fulfillment: 'delivery', memberships: [], session_id: await digest(scope)};
        return {retailer_id: id, name: names[id], logged_in: data != null, context};
      }
      const [customer, cart] = await Promise.all([request(`${base}/customer`), getCart()]);
      const storeID = String(customer.storeId || cart.deliveryTimeSlot?.slotBaseStoreId || cart.deliveryTimeSlot?.pickupStoreId || '');
      if (!storeID) throw new Error(`Select a store and delivery or pickup on ${names[id]}.`);
      const mode = String(cart.deliveryModeCode || '').toLowerCase();
      const fulfillment = mode.includes('delivery') || cart.deliverySlot === true ? 'delivery' : mode.includes('pickup') || mode.includes('collect') ? 'pickup' : '';
      if (!fulfillment) throw new Error(`Choose delivery or pickup on ${names[id]}.`);
      const context = {store_id: storeID, postal_code: postal(cart.postalCode || cart.deliveryAddress?.postalCode || customer.postalCode), fulfillment, delivery_slot_id: cart.slotCode || '', memberships: [], session_id: await digest({account: customer.uid, storeID, mode: cart.deliveryModeCode, slot: cart.slotCode, postal: cart.postalCode || cart.deliveryAddress?.postalCode})};
      return {retailer_id: id, name: `${names[id]} · ${storeID}`, logged_in: customer.uid !== 'anonymous' && Boolean(customer.uid), context};
    }
    async function check(context, login = false) {
      const current = await session();
      if (!sameContext(current.context, context)) throw new Error('Your store session changed. Create a new plan.');
      if (login && !current.logged_in) throw Object.assign(new Error(`Sign in on ${names[id]}.`), {status: 'login_required'});
      return current;
    }
    async function search(job) {
      await check(job.context);
      const page = job.cursor === undefined || job.cursor === '' ? 0 : Number(job.cursor);
      if (!Number.isInteger(page) || page < 0 || page > 24) throw new Error('Invalid search cursor.');
      const query = new URLSearchParams(id === 'mathem' ? {q: job.query, page: String(page + 1)} : {q: job.query, page: String(page), size: '40'});
      const path = id === 'mathem' ? `/api/v1/search/?${query}` : id === 'willys' ? `${base}/search?${query}` : `/search/asutype?${query}`;
      const data = await request(path);
      const rows = id === 'mathem' ? data.products : data.results;
      if (!Array.isArray(rows)) throw new Error('Retailer search format changed.');
      const products = rows.map(id === 'mathem' ? mathemProduct : axfoodProduct).filter(Boolean);
      const more = id === 'mathem' ? (page + 1) * 40 < data.attributes?.total_hits : page + 1 < data.pagination?.numberOfPages;
      await check(job.context);
      return {retailer_id: id, context: job.context, observed_at: new Date().toISOString(), products, ...(more ? {next_cursor: String(page + 1)} : {})};
    }
    function quantities(cart) {
      const rows = id === 'mathem' ? (cart.items || cart.groups?.flatMap((group) => group.items)) : cart.products;
      if (!Array.isArray(rows)) throw new Error('Retailer basket format changed.');
      const result = {};
      for (const row of rows) {
        const code = id === 'mathem' ? row.product?.id : row.code;
        const quantity = id === 'mathem' ? row.quantity : number(row.pickQuantity) > 0 ? row.pickQuantity : row.quantity;
        if (code == null || !Number.isFinite(number(quantity)) || number(quantity) < 0) throw new Error('Invalid basket quantity.');
        result[String(code)] = (result[String(code)] || 0) + number(quantity);
      }
      return result;
    }
    async function read(context) {
      await check(context, true);
      const cart = await getCart();
      if (cart.orderReference || cart.notEditableAfterOrder || cart.frozen) throw new Error('This basket is an existing order or cannot be edited.');
      return quantities(cart);
    }
    async function set(context, item, before) {
      await check(context, true);
      if (id === 'mathem') {
        if (!/^\d+$/.test(item.product_id)) throw new Error('Invalid Mathem product ID.');
        const current = quantities(await getCart())[item.product_id] || 0;
        if (current !== before) throw new Error('The basket changed during transfer.');
        const delta = item.target_quantity - current;
        if (delta !== 0) await request('/api/v1/cart/items/?group-by=none', {method: 'POST', body: {items: [{productId: Number(item.product_id), quantity: delta}]}});
      } else {
        if (!/^\d+_(ST|KG)$/.test(item.product_id)) throw new Error('Invalid Axfood product ID.');
        const cart = await getCart();
        const old = cart.products?.find((row) => row.code === item.product_id);
        const product = old || await request(`${base}/p/${encodeURIComponent(item.product_id)}`);
        if (!['ST', 'KG'].includes(product.productBasketType?.code)) throw new Error('The retailer basket unit could not be verified.');
        const pickUnit = product.productBasketType.code === 'KG' ? 'kilogram' : 'pieces';
        await request(`${base}/cart/addProducts`, {method: 'POST', body: {storeId: context.store_id, products: [{productCodePost: item.product_id, qty: item.target_quantity, pickUnit}]}});
      }
    }
    return {session, search, read, set, check};
  }
  globalThis.ShipItAdapters = {origins, names, create, pack, money, traits, axfoodProduct, mathemProduct, sameContext};
})();
