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
      if (cart.orderReference || cart.notEditableAfterOrder || cart.frozen) throw new Error('This basket is an existing order or cannot be edited.');
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
    return {read, set};
  }
  globalThis.ShipItAdapters = {origins, names, create};
})();
