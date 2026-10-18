// Shared script prelude for transforms and response handlers.
//
// The server (internal/wasm/runner.go, QuickJS via fastschema/qjs) and the
// browser (web/src/lib/wasm.ts, quickjs-emscripten) both load this file ahead
// of the user's script, so a script behaves the same in server mode, browser
// mode and the editor preview. Names start with __th_ so user code doesn't
// collide with them.
//
// What a script sees on `req`:
//   req.method, req.path, req.query, req.content_type
//   req.headers           { "Name": ["value", ...] }
//   req.body              the raw body as a string
//   req.json              the body parsed as JSON, or null when it isn't JSON
//   req.header(name)      first value of a header, any case; undefined if absent
//   req.setHeader(n, v)   replace a header
//   req.removeHeader(n)   delete a header
//
// A transform returns the request to forward (changes to req.json are written
// back to the body), or null to skip forwarding it.

// Scripts get plain JavaScript only. qjs adds host modules (std, os) to the
// global scope; remove them before any user code runs. The runner also gives
// the runtime an empty, read-only filesystem, so this isn't the only barrier.
(function () {
  var names = ['std', 'os', 'bjson', 'scriptArgs', 'print'];
  for (var i = 0; i < names.length; i++) {
    try { delete globalThis[names[i]]; } catch (e) {}
    if (typeof globalThis[names[i]] !== 'undefined') {
      try { Object.defineProperty(globalThis, names[i], { value: undefined, writable: false, configurable: false }); } catch (e) {}
    }
  }
})();

function __th_headerKeys(headers, name) {
  var want = String(name).toLowerCase();
  var keys = [];
  for (var k in headers) {
    if (Object.prototype.hasOwnProperty.call(headers, k) && k.toLowerCase() === want) keys.push(k);
  }
  return keys;
}

function __th_prepare(input) {
  var req = input || {};
  if (!req.headers || typeof req.headers !== 'object') req.headers = {};
  if (typeof req.body !== 'string') req.body = req.body == null ? '' : String(req.body);

  var json = null;
  if (req.body !== '') {
    try { json = JSON.parse(req.body); } catch (e) { json = null; }
  }
  req.json = json;

  // Hidden, so they don't show up in what the script returns.
  Object.defineProperty(req, '__th_json_snapshot', {
    value: json === null ? null : JSON.stringify(json),
    enumerable: false
  });
  Object.defineProperty(req, 'header', {
    enumerable: false,
    value: function (name) {
      var keys = __th_headerKeys(this.headers, name);
      if (keys.length === 0) return undefined;
      var v = this.headers[keys[0]];
      return Array.isArray(v) ? v[0] : v;
    }
  });
  Object.defineProperty(req, 'setHeader', {
    enumerable: false,
    value: function (name, value) {
      var keys = __th_headerKeys(this.headers, name);
      for (var i = 0; i < keys.length; i++) delete this.headers[keys[i]];
      this.headers[name] = [String(value)];
      return this;
    }
  });
  Object.defineProperty(req, 'removeHeader', {
    enumerable: false,
    value: function (name) {
      var keys = __th_headerKeys(this.headers, name);
      for (var i = 0; i < keys.length; i++) delete this.headers[keys[i]];
      return this;
    }
  });
  return req;
}

// Turns what transform() returned into the request to forward.
function __th_finishTransform(out, req) {
  if (out === null) return { drop: true };
  if (out === undefined) {
    throw new Error('transform() returned nothing. Return req to forward it, or null to skip it.');
  }
  if (typeof out !== 'object' || Array.isArray(out)) {
    throw new Error('transform() must return the request object (or null), not ' + (Array.isArray(out) ? 'an array' : typeof out) + '.');
  }
  if (Object.prototype.hasOwnProperty.call(out, 'json')) {
    var json = out.json;
    var snapshot = req && req.__th_json_snapshot !== undefined ? req.__th_json_snapshot : null;
    var serialized = json === null || json === undefined ? null : JSON.stringify(json);
    // Write req.json back to the body only when the script changed it, so an
    // untouched body keeps its exact original bytes and formatting.
    if (serialized !== null && serialized !== snapshot) {
      out.body = serialized;
      if (!out.content_type) out.content_type = 'application/json';
    }
    delete out.json;
  }
  return out;
}

// Builds the transform program's result as a JSON string.
function __th_runTransform(input) {
  var req = __th_prepare(input);
  if (typeof transform !== 'function') return JSON.stringify(__th_finishTransform(req, req));
  return JSON.stringify(__th_finishTransform(transform(req), req));
}

// Builds the response handler's result as a JSON string (null = default response).
function __th_runHandler(input) {
  var req = __th_prepare(input);
  if (typeof handler !== 'function') return 'null';
  var res = handler(req);
  return JSON.stringify(res === undefined ? null : res);
}
