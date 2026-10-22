'use strict';

(() => {
  const origins = {willys: 'https://www.willys.se', hemkop: 'https://www.hemkop.se', mathem: 'https://www.mathem.se'};
  const names = {willys: 'Willys', hemkop: 'Hemköp', mathem: 'Mathem'};
  const number = (value) => Number(String(value).replaceAll(' ', '').replace(',', '.'));
  function create(id, transport) {
    if (!origins[id]) throw new Error('Unsupported retailer.');
    const request = transport.request;
    const base = id === 'willys' ? '/axfood/rest/v2' : '/axfood/rest';
    const getCart = () => request(id === 'mathem' ? '/api/v1/cart/?group-by=none' : `${base}/cart`);
    const unavailable = () => Object.assign(new Error(`Not sold online at your ${names[id]} store.`), {status: 'unavailable'});
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
    async function read() {
      const cart = await getCart();
      if (cart.orderReference || cart.notEditableAfterOrder) throw new Error('This basket is an existing order or cannot be edited.');
      return quantities(cart);
    }
    async function set(item, before) {
      if (id === 'mathem') {
        if (!/^\d+$/.test(item.product_id)) throw new Error('Invalid Mathem product ID.');
        const current = quantities(await getCart())[item.product_id] || 0;
        if (current !== before) throw new Error('The basket changed during transfer.');
        const delta = item.target_quantity - current;
        if (delta !== 0) await request('/api/v1/cart/items/?group-by=none', {method: 'POST', body: {items: [{productId: Number(item.product_id), quantity: delta}]}});
        return;
      }
      if (!/^\d+_(ST|KG)$/.test(item.product_id)) throw new Error('Invalid Axfood product ID.');
      const cart = await getCart();
      const product = cart.products?.find((row) => row.code === item.product_id) || await request(`${base}/p/${encodeURIComponent(item.product_id)}`).catch((error) => { throw error.httpStatus === 404 ? unavailable() : error; });
      if (!['ST', 'KG'].includes(product.productBasketType?.code)) throw new Error('The retailer basket unit could not be verified.');
      const pickUnit = product.productBasketType.code === 'KG' ? 'kilogram' : 'pieces';
      await request(`${base}/cart/addProducts`, {method: 'POST', body: {products: [{productCodePost: item.product_id, qty: item.target_quantity, pickUnit}]}}).catch((error) => { throw /not\.saleable/.test(error.body || '') ? unavailable() : error; });
    }
    const blankContext = () => ({store_id: '', postal_code: '', fulfillment: ''});
    async function session() {
      try {
        if (id === 'mathem') {
          const slots = await request('/api/v1/slot-picker/slots/?num-days=1');
          const selected = slots.cartInfo?.deliveryAddress;
          if (!selected?.id) return {status: 'setup_required', label: names[id], context: blankContext()};
          const address = await request(`/api/v1/user/addresses/address/?user-delivery-address-id=${encodeURIComponent(selected.id)}`);
          const postal = String(address.zipCode || '').replaceAll(' ', '');
          const context = {session_id: `address:${selected.id}`, store_id: 'mathem', postal_code: postal, fulfillment: 'delivery'};
          if (slots.cartInfo?.deliverySlot?.id) context.delivery_slot_id = String(slots.cartInfo.deliverySlot.id);
          return {status: /^\d{5}$/.test(postal) ? 'ready' : 'setup_required', label: `Mathem · ${postal}`, context};
        }
        const [cart, store] = await Promise.all([getCart(), request(`${base}/store/active`)]);
        const mode = cart.deliveryModeCode;
        const fulfillment = mode === 'homeDelivery' || mode === 'homeDeliveryB2b' ? 'delivery' : mode === 'pickupInStore' ? 'pickup' : '';
        const address = fulfillment === 'delivery' ? cart.deliveryAddress : store.address;
        const postal = String(address?.postalCode || cart.postalCode || '').replaceAll(' ', '');
        const context = {store_id: String(store.storeId || ''), postal_code: postal, fulfillment};
        if (cart.deliveryTimeSlot?.code) context.delivery_slot_id = String(cart.deliveryTimeSlot.code);
        return {status: store.activelySelected === true && context.store_id && fulfillment && /^\d{5}$/.test(postal) ? 'ready' : 'setup_required', label: store.name || names[id], context};
      } catch (error) {
        if (error.status === 'login_required') return {status: 'login_required', label: names[id], context: blankContext()};
        throw error;
      }
    }
    async function search(input) {
      const before = await session();
      if (before.status !== 'ready') throw new Error('Choose your store or delivery settings at the retailer, then check the connection again.');
      if (JSON.stringify(before.context) !== JSON.stringify(input.context)) throw new Error('Your store settings changed. Check the store connection again.');
      if (typeof input.query !== 'string' || !input.query.trim() || input.query.length > 200 || !Number.isInteger(input.page) || input.page < 0 || input.page > 24) throw new Error('Invalid store search.');
      const query = new URLSearchParams({q: input.query, page: String(id === 'mathem' ? input.page + 1 : input.page)});
      if (id !== 'mathem') query.set('size', '40');
      const result = await request(`${id === 'mathem' ? '/api/v1/search/' : '/axfood/rest/v2/search'}?${query}`);
      const after = await session();
      if (JSON.stringify(after.context) !== JSON.stringify(before.context)) throw new Error('Your store settings changed during search. Check the store connection again.');
      return result;
    }
    return {read, set, session, search};
  }
  globalThis.ShipItAdapters = {origins, names, create};
})();
