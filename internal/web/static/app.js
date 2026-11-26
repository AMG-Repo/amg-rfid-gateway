(() => {
  // ── constants ──────────────────────────────────────────────
  var TOKEN_KEY = "amgWebAuthToken";
  var THEME_KEY = "amg-theme";
  var COMPACT_AT = 8; // lanes with more cards switch to one-line rows
  var REARM_GAP_MS = 30000; // a confirmed tag read again after this gap is a new movement
  var UNASSIGNED_GRACE_MS = 60000; // keep a live-read unassigned tag while the inbox catches up
  var POLL_MS = 15000;
  var PICKER_MAX = 50;

  var MOV = {
    entrada: {
      arrow: "↓",
      noun: "Entrada",
      plural: "entradas",
      cls: "in",
      btn: "Confirmar entrada",
    },
    salida: {
      arrow: "↑",
      noun: "Salida",
      plural: "salidas",
      cls: "out",
      btn: "Confirmar salida",
    },
  };
  var LABEL = { all: "Todas", entrada: "Entradas", salida: "Salidas" };
  var SYNC_OK = "Enviada al VPS";
  var SYNC_PENDING = "Guardada en el gateway · se enviará al VPS";

  // ── state ──────────────────────────────────────────────────
  var cards = new Map(); // epc -> card (one card per EPC, updated in place)
  var filter = "all";
  var single = false;
  var wideCols = false; // filtered + compact lane: rows get the extra columns
  var open = new Set(); // keys of expanded drawers, survive re-renders
  var busy = false; // a confirmation request is in flight
  var progress = null; // {mov, n, total} while a batch runs
  var uid = 0;
  var requiresBearer = false;
  var sseUp = false;
  var status = null; // last /api/status
  var statusErr = false;
  var antennas = null; // last /api/antennas
  var history = null; // last /api/history
  var kanbans = null; // cached /api/kanbans (picker source)
  var lastIssue = "";
  var toastTimer;
  var narrow = window.matchMedia("(max-width: 899px)");

  // ── tiny DOM helpers (text nodes only, never markup strings) ──
  function $(id) {
    return document.getElementById(id);
  }
  function h(tag, cls, text, attrs) {
    var el = document.createElement(tag);
    if (cls) el.className = cls;
    if (text !== undefined) el.textContent = text;
    if (attrs)
      Object.keys(attrs).forEach((k) => {
        el.setAttribute(k, attrs[k]);
      });
    return el;
  }
  function setNodes(el, parts) {
    el.replaceChildren.apply(el, parts);
  }
  function other(m) {
    return m === "entrada" ? "salida" : "entrada";
  }
  function uniq(a) {
    return a.filter((v, i) => a.indexOf(v) === i);
  }
  // Keep short values together so a wrapped meta line breaks only at " · ".
  function nb(v) {
    return String(v).replace(/ /g, "\u00a0");
  }
  function baseOf(k) {
    return k.replace(/\s+#\d+$/, "");
  }
  function qtyText(n) {
    if (typeof n !== "number" || !isFinite(n)) return "";
    return Number.isInteger(n) ? String(n) : String(Math.round(n * 100) / 100);
  }
  function hhmm(ts, seconds) {
    var d = new Date(ts);
    if (isNaN(d.getTime())) return "";
    var o = { hour: "2-digit", minute: "2-digit", hour12: false };
    if (seconds) o.second = "2-digit";
    return d.toLocaleTimeString("es", o);
  }
  function parseTs(v) {
    var t = v ? Date.parse(v) : NaN;
    return isNaN(t) ? null : t;
  }
  function fmtAgo(ts) {
    if (ts == null) return "";
    var s = Math.max(0, Math.round((Date.now() - ts) / 1000));
    if (s < 60) return "hace " + s + " s";
    var m = Math.floor(s / 60);
    if (m < 60) return "hace " + m + " min";
    var hr = Math.floor(m / 60);
    if (hr < 24) return "hace " + hr + " h";
    return "hace " + Math.floor(hr / 24) + " d";
  }
  // A live "hace N s" span. epcs: cards whose latest read drives it; ts: fixed time.
  function agoSpan(epcs, ts) {
    var s = h("span", "ago");
    if (epcs) s.dataset.epcs = epcs.join(",");
    else s.dataset.ts = String(ts);
    s.textContent = fmtAgo(agoValue(s));
    return s;
  }
  function agoValue(e) {
    if (e.dataset.epcs) {
      var latest = null;
      e.dataset.epcs.split(",").forEach((epc) => {
        var c = cards.get(epc);
        if (c && (latest === null || c.readAt > latest)) latest = c.readAt;
      });
      return latest;
    }
    return Number(e.dataset.ts);
  }
  function tickAgo() {
    document.querySelectorAll(".ago").forEach((e) => {
      var t = fmtAgo(agoValue(e));
      if (e.textContent !== t) e.textContent = t;
    });
    $("clock").textContent = hhmm(Date.now(), true);
  }
  function say(msg) {
    var live = $("live");
    live.textContent = "";
    requestAnimationFrame(() => {
      live.textContent = msg;
    });
  }

  // ── auth ───────────────────────────────────────────────────
  function getToken() {
    try {
      return sessionStorage.getItem(TOKEN_KEY) || "";
    } catch (e) {
      return "";
    }
  }
  function setToken(t) {
    try {
      if (t) sessionStorage.setItem(TOKEN_KEY, t);
      else sessionStorage.removeItem(TOKEN_KEY);
    } catch (e) {}
  }
  function canWrite() {
    return !requiresBearer || Boolean(getToken());
  }
  var authReady = fetch("/api/auth-mode")
    .then((r) => (r.ok ? r.json() : null))
    .then((d) => {
      if (d) requiresBearer = Boolean(d.requires_bearer);
    })
    .catch(() => {})
    .then(() => {
      syncTokenForm();
    });

  function syncTokenForm(errMsg) {
    var form = $("token");
    var need = requiresBearer && !getToken();
    form.hidden = !need;
    var err = $("tokerr");
    err.hidden = !errMsg;
    err.textContent = errMsg || "";
    render();
    renderStatus();
  }
  function onUnauthorized() {
    setToken("");
    var msg = "Token LAN inválido o faltante. Ingresa el token correcto.";
    requiresBearer = true;
    syncTokenForm(msg);
    say(msg);
    $("tok").focus();
  }
  async function ensureAuth() {
    await authReady;
    if (canWrite()) return true;
    syncTokenForm();
    say("Ingresa el token web para confirmar movimientos.");
    $("tok").focus();
    return false;
  }
  async function post(url, body) {
    var headers = { "Content-Type": "application/json" };
    var t = getToken();
    if (t) headers.Authorization = "Bearer " + t;
    var res = await fetch(url, {
      method: "POST",
      headers: headers,
      body: JSON.stringify(body),
    });
    var data = null;
    try {
      data = await res.json();
    } catch (e) {}
    return { status: res.status, ok: res.ok, data: data || {} };
  }

  // ── cards ──────────────────────────────────────────────────
  function newCard(epc) {
    return {
      epc: epc,
      ant: "",
      rssi: null,
      readAt: Date.now(),
      kanban: null,
      unassigned: false,
      sug: "entrada",
      reason: "",
      mov: "entrada",
      moved: false,
      status: "pending", // pending | done | fail
      offline: false,
      failMsg: "",
      doneAt: 0,
      fresh: true,
    };
  }
  function applyEvent(c, d) {
    c.ant = d.antenna_id || c.ant;
    c.rssi = typeof d.rssi === "number" ? d.rssi : null;
    c.readAt = Date.now();
    c.kanban = d.kanban || null;
    c.unassigned = Boolean(d.unassigned) && !d.kanban;
    c.sug = d.suggested_action === "salida" ? "salida" : "entrada";
    c.reason = d.suggested_reason || "";
    c.mov = c.sug;
    c.moved = false;
  }
  // Returns true when the list structure changed (needs a re-render).
  function onTag(d) {
    if (!d || !d.epc) return false;
    var c = cards.get(d.epc);
    if (!c) {
      c = newCard(d.epc);
      applyEvent(c, d);
      cards.set(c.epc, c);
      return true;
    }
    var now = Date.now();
    // A confirmed tag that left the antenna and came back is a new movement.
    if (c.status === "done" && now - c.readAt >= REARM_GAP_MS) {
      applyEvent(c, d);
      c.status = "pending";
      c.failMsg = "";
      c.fresh = true;
      return true;
    }
    var structural =
      Boolean(d.kanban) !== Boolean(c.kanban) ||
      (Boolean(d.unassigned) && !d.kanban) !== c.unassigned;
    if (structural && c.status !== "done") {
      var keepMov = c.moved ? c.mov : null;
      applyEvent(c, d);
      if (keepMov) {
        c.mov = keepMov;
        c.moved = true;
      }
      return true;
    }
    // Repeat read: update in place; the list is never rebuilt for it.
    c.readAt = now;
    c.rssi = typeof d.rssi === "number" ? d.rssi : c.rssi;
    return false;
  }

  function info(c) {
    var k = c.kanban || {};
    return {
      code: k.code || c.epc,
      hasKanban: Boolean(k.code),
      desc: k.description || "",
      qty: typeof k.standard_pack === "number" ? k.standard_pack : undefined,
      unit: k.unit || "",
      cust: k.customer || "",
      ext: k.extrusora || "",
      pos: k.position || "",
      area: k.area || "",
      zones: k.zones || [],
      sku: k.sku || "",
    };
  }
  function zoneText(i) {
    if (!i.zones.length) return "";
    return (
      (i.zones.length === 1 ? "Zona\u00a0" : "Zonas\u00a0") + i.zones.join(", ")
    );
  }
  function metaText(i) {
    return [i.ext, zoneText(i)].filter(Boolean).join(" · ");
  }
  function sumQty(cs) {
    return cs.reduce((a, c) => a + (info(c).qty || 0), 0);
  }
  function totals(cs) {
    var o = {};
    cs.forEach((c) => {
      var i = info(c);
      if (i.qty === undefined) return;
      o[i.unit] = (o[i.unit] || 0) + i.qty;
    });
    var out = Object.keys(o).map((u) => (qtyText(o[u]) + " " + u).trim());
    return out.length ? out.join(" · ") : "—";
  }
  function reasonText(c) {
    var i = info(c);
    var r = c.reason || "";
    if (c.sug === "entrada" && i.ext && r.indexOf("zone:") !== 0)
      return "Viene de la extrusora " + i.ext + ".";
    if (r === "zone:entrada") return "La antena " + c.ant + " es de entrada.";
    if (r === "zone:salida") return "La antena " + c.ant + " es de salida.";
    if (r === "location:warehouse") return "Figura dentro del almacén.";
    if (r === "location:outside") return "Figura fuera del almacén.";
    return "Sin historial de este tag; se sugiere entrada.";
  }
  function whyText(c) {
    if (c.status === "fail")
      return c.failMsg || "No se guardó el movimiento. Nada se envió.";
    if (c.mov !== c.sug)
      return (
        "Lo moviste manualmente a " +
        MOV[c.mov].plural +
        ". El sistema sugería " +
        c.sug +
        "."
      );
    return reasonText(c);
  }

  // ── confirm ────────────────────────────────────────────────
  // Returns "ok" | "fail" | "full" | "auth".
  async function confirmCard(c) {
    var r;
    try {
      r = await post("/api/confirm", {
        uii: c.epc,
        action: c.mov,
        antenna_id: c.ant,
      });
    } catch (e) {
      c.status = "fail";
      c.failMsg =
        "Sin respuesta del gateway. Revisa la conexión e intenta de nuevo.";
      return "fail";
    }
    if (r.status === 401) {
      onUnauthorized();
      return "auth";
    }
    if (r.ok && r.data.success) {
      c.status = "done";
      c.doneAt = Date.now();
      c.offline = r.data.mode === "offline";
      c.failMsg = "";
      return "ok";
    }
    if (r.data.code === "tag_not_assigned") {
      // The link is gone: back to the attention zone to be assigned.
      c.unassigned = true;
      c.kanban = null;
      c.status = "pending";
      c.failMsg = r.data.message || "";
      return "fail";
    }
    c.status = "fail";
    c.failMsg =
      r.data.message || r.data.error || "No se pudo guardar el movimiento.";
    return r.data.code === "queue_full" ? "full" : "fail";
  }

  // Sequential POST per card. Stops on auth errors and when the queue is full.
  async function runConfirm(items, mov, focusSel) {
    if (busy) return;
    if (!(await ensureAuth())) return;
    var todo = items.filter((c) => c.status !== "done" && !c.unassigned);
    if (!todo.length) return;
    busy = true;
    progress = todo.length > 1 ? { mov: mov, n: 0, total: todo.length } : null;
    var ok = 0;
    var failed = 0;
    var stopped = "";
    for (var i = 0; i < todo.length; i++) {
      render();
      var res = await confirmCard(todo[i]);
      if (progress) progress.n++;
      if (res === "ok") ok++;
      else failed++;
      if (res === "auth" || res === "full") {
        stopped = res;
        break;
      }
    }
    busy = false;
    progress = null;
    render(focusSel);
    loadHistory();
    loadStatus();
    var left = todo.length - ok - failed;
    var msg = ok + (ok === 1 ? " confirmada" : " confirmadas");
    if (failed)
      msg +=
        "; " +
        failed +
        (failed === 1 ? " requiere" : " requieren") +
        " atención";
    if (stopped === "full")
      msg +=
        ". La cola del gateway está llena, quedan " + left + " sin confirmar";
    if (todo.length === 1 && failed) msg = todo[0].failMsg || msg;
    say(msg);
  }

  // ── building blocks ────────────────────────────────────────
  function goBtn(c) {
    var i = info(c);
    var m = MOV[c.mov];
    var fail = c.status === "fail";
    var b = h(
      "button",
      "go " + (fail ? "btn-primary" : m.cls === "in" ? "btn-in" : "btn-out"),
      fail ? "Reintentar" : "Confirmar",
      {
        type: "button",
        "data-confirm": "1",
        "data-fk": "go:" + c.epc,
        "aria-label": (fail ? "Reintentar " : m.btn + " de ") + i.code,
      },
    );
    b.addEventListener("click", () => {
      runConfirm([c], c.mov, '[data-epc="' + c.epc + '"] .go');
    });
    return b;
  }
  function moveBtn(c) {
    var i = info(c);
    var b = h(
      "button",
      "move btn-ghost",
      "Pasar a " + MOV[other(c.mov)].plural,
      {
        type: "button",
        "data-fk": "mv:" + c.epc,
        "aria-label": "Pasar a " + MOV[other(c.mov)].plural + ": " + i.code,
      },
    );
    b.addEventListener("click", () => {
      // If it lands in a group of the other lane, keep that group open so
      // focus has somewhere to go.
      open.add("g:" + other(c.mov) + ":" + baseOf(i.code));
      c.mov = other(c.mov);
      c.moved = c.mov !== c.sug;
      render('[data-epc="' + c.epc + '"] .go');
      // The card left the visible lane: say where it went instead of vanishing.
      if (filter !== "all" && filter !== c.mov) announceMove(c);
    });
    return b;
  }
  function assignBtn(label, aria, c, fk) {
    var b = h("button", "btn-warn", label, {
      type: "button",
      "data-confirm": "1",
      "data-fk": fk,
      "aria-label": aria,
    });
    b.addEventListener("click", () => {
      openPicker(c, fk);
    });
    return b;
  }
  function attrsLine(i) {
    var p = h("p", "attrs");
    [i.cust, i.ext && "Extrusora " + i.ext, i.pos && "Pos. " + i.pos, i.area]
      .filter(Boolean)
      .forEach((v) => {
        p.append(h("span", "", v));
      });
    if (i.zones.length)
      p.append(h("span", "z", zoneText(i).replace("\u00a0", " ")));
    return p.childNodes.length ? p : null;
  }
  // Full detail of one physical card: shared by the slip and the row drawer.
  function parts(c) {
    var i = info(c);
    var out = [];
    if (i.desc) out.push(h("p", "desc", i.desc));
    var a = attrsLine(i);
    if (a) out.push(a);
    out.push(h("p", "why" + (c.status === "fail" ? " t-bad" : ""), whyText(c)));
    var tl = h("p", "tagline");
    var bits = [];
    if (i.sku) bits.push("P/N " + i.sku);
    bits.push("Tag " + c.epc);
    if (c.ant) bits.push(c.ant);
    tl.append(bits.join(" · ") + " · ", agoSpan([c.epc]));
    out.push(tl);
    return out;
  }
  function slip(c) {
    var i = info(c);
    var li = h("li", "slip" + (c.fresh ? " new-card" : ""));
    li.dataset.epc = c.epc;
    var top = h("div", "s-top");
    top.append(h("h4", "kan" + (i.hasKanban ? "" : " mono"), i.code));
    if (i.qty !== undefined) {
      var q = h("span", "qty", qtyText(i.qty));
      if (i.unit) q.append(h("small", "", " " + i.unit));
      top.append(q);
    }
    var act = h("div", "s-act");
    act.append(goBtn(c), moveBtn(c));
    li.append(top);
    parts(c).forEach((p) => {
      li.append(p);
    });
    li.append(act);
    return li;
  }

  // Columns of the wide row (single lane).
  var COL_LABELS = [
    "Descripción",
    "Cliente",
    "Extrusora",
    "Posición",
    "Zona",
    "Lectura",
  ];
  function xcols(v) {
    return [
      [v.desc, "xd"],
      [v.cust],
      [v.ext],
      [v.pos],
      [v.zone],
      [null, "", v.epcs],
    ];
  }
  function colHead() {
    var d = h("div", "row chead", undefined, { "aria-hidden": "true" });
    var main = h("div", "row-main");
    var lead = h("div", "row-toggle");
    lead.append(h("span"), h("span", "", "Kanban"), h("span", "", "Cant."));
    COL_LABELS.forEach((c) => {
      lead.append(h("span", "xc", c));
    });
    main.append(lead, h("div", "row-act"));
    d.append(main);
    return d;
  }

  // One-line row; the full detail lives in a drawer toggled by the row button.
  function row(o) {
    var li = h(
      "li",
      "row" +
        (o.cls ? " " + o.cls : "") +
        (o.qty === undefined ? " noq" : "") +
        (o.xcols ? " x" : "") +
        (o.fresh ? " new-card" : ""),
    );
    if (o.epc) li.dataset.epc = o.epc;
    var id = "dt" + ++uid;
    var isOpen = Boolean(o.detail && open.has(o.key));
    var lead = o.detail
      ? h("button", "row-toggle", undefined, {
          type: "button",
          "aria-expanded": String(isOpen),
          "aria-controls": id,
          "data-fk": "tg:" + (o.key || o.epc),
        })
      : h("div", "row-toggle static");
    var kan = h("span", "kan" + (o.mono ? " mono" : ""), o.kan);
    if (o.grp) kan.append(h("span", "grp", " " + o.grp));
    lead.append(
      h("span", "chev", o.detail ? "▸" : "", { "aria-hidden": "true" }),
      kan,
    );
    if (o.qty !== undefined) {
      var q = h("span", "rq", qtyText(o.qty));
      if (o.unit) q.append(h("small", "", " " + o.unit));
      lead.append(q);
    }
    var meta = h("span", "meta" + (o.metaCls ? " " + o.metaCls : ""));
    var metaParts = [].concat(o.meta);
    metaParts.forEach((p) => {
      meta.append(p);
    });
    if (metaParts.every((p) => typeof p === "string"))
      meta.title = metaParts.join("");
    lead.append(meta);
    if (o.xcols)
      o.xcols.forEach((col) => {
        var x = h("span", "xc" + (col[1] ? " " + col[1] : ""));
        if (col[2]) x.append(agoSpan(col[2]));
        else {
          x.textContent = col[0] || "";
          x.title = col[0] || "";
        }
        lead.append(x);
      });
    var main = h("div", "row-main");
    main.append(lead);
    if (o.acts && o.acts.length) {
      var a = h("div", "row-act");
      o.acts.forEach((b) => {
        a.append(b);
      });
      main.append(a);
    }
    li.append(main);
    if (o.detail) {
      var det = h("div", "row-detail", undefined, { id: id });
      det.hidden = !isOpen;
      o.detail.forEach((n) => {
        det.append(n);
      });
      lead.addEventListener("click", () => {
        var was = lead.getAttribute("aria-expanded") === "true";
        lead.setAttribute("aria-expanded", String(!was));
        det.hidden = was;
        if (was) open.delete(o.key);
        else open.add(o.key);
      });
      li.append(det);
    }
    return li;
  }
  function oneRow(c, nested) {
    var i = info(c);
    var fail = c.status === "fail";
    var meta;
    if (fail) meta = ["✕ No se guardó"];
    else if (nested)
      meta = [nb(i.pos ? "Pos. " + i.pos + " · " : ""), agoSpan([c.epc])];
    else meta = [metaText(i)];
    return row({
      key: "r:" + c.epc,
      epc: c.epc,
      fresh: c.fresh,
      cls: fail ? "fail wide" : "",
      kan: i.code,
      mono: !i.hasKanban,
      qty: i.qty,
      unit: i.unit,
      meta: meta,
      metaCls: fail ? "t-bad" : "",
      acts: fail ? [goBtn(c)] : [moveBtn(c), goBtn(c)],
      detail: parts(c),
      xcols:
        wideCols && !nested && !fail
          ? xcols({
              desc: i.desc,
              cust: i.cust,
              ext: i.ext,
              pos: i.pos,
              zone: i.zones.join(", "),
              epcs: [c.epc],
            })
          : undefined,
    });
  }
  // Physical cards of one kanban share its base code ("EX550D4", "EX550D4 #2").
  function groupRow(g, mov) {
    var m = MOV[mov];
    var cs = g.cs;
    var n = cs.length;
    var is = cs.map(info);
    var exts = uniq(is.map((i) => i.ext));
    var zs = uniq(is.map((i) => i.zones.join(", ")));
    var go = h(
      "button",
      "gogrp " + (m.cls === "in" ? "btn-in" : "btn-out"),
      "Confirmar ×" + n,
      {
        type: "button",
        "data-confirm": "1",
        "data-fk": "gg:" + mov + ":" + g.base,
        "aria-label":
          "Confirmar las " + n + " tarjetas de " + g.base + " como " + m.plural,
      },
    );
    go.addEventListener("click", () => {
      runConfirm(cs, mov, "#h-" + mov);
    });
    var sub = h("ul", "sub");
    cs.forEach((c) => {
      sub.append(oneRow(c, true));
    });
    var epcs = cs.map((c) => c.epc);
    return row({
      key: "g:" + mov + ":" + g.base,
      kan: g.base,
      grp: "×" + n,
      qty: sumQty(cs) || undefined,
      unit: is[0].unit,
      meta: [
        nb(
          [
            exts.length === 1 ? exts[0] : "varias extr.",
            zs.length === 1 ? (zs[0] ? "Zona " + zs[0] : "") : "varias zonas",
          ]
            .filter(Boolean)
            .join(" · "),
        ),
      ],
      acts: wideCols
        ? [h("span", "ph", undefined, { "aria-hidden": "true" }), go]
        : [go],
      xcols: wideCols
        ? xcols({
            desc: is[0].desc,
            cust:
              uniq(is.map((i) => i.cust)).length === 1 ? is[0].cust : "varios",
            ext: exts.length === 1 ? exts[0] : "varias",
            pos: uniq(is.map((i) => i.pos)).length === 1 ? is[0].pos : "varias",
            zone: zs.length === 1 ? zs[0] : "varias",
            epcs: epcs,
          })
        : undefined,
      detail: [
        h("p", "desc", is[0].desc),
        h(
          "p",
          "why",
          n +
            " tarjetas físicas del mismo kanban. «Confirmar ×" +
            n +
            "» las confirma todas; «Pasar» actúa sobre cada una.",
        ),
        sub,
      ],
    });
  }
  function unRow(un) {
    var n = un.length;
    var epcs = un.map((c) => c.epc);
    function tagMeta(c) {
      var p = [c.ant, " · ", agoSpan([c.epc])];
      // rssi is signed dBm; 0 means unknown, so it is not shown.
      if (c.rssi !== null && c.rssi !== 0) p.push(" · " + c.rssi + " dBm");
      return p;
    }
    var sub = h("ul", "sub");
    un.forEach((c) => {
      sub.append(
        row({
          epc: c.epc,
          mono: true,
          kan: c.epc,
          meta: tagMeta(c),
          acts: [
            assignBtn("Asignar", "Asignar el tag " + c.epc, c, "as:" + c.epc),
          ],
        }),
      );
    });
    var acts;
    if (n === 1) {
      acts = [
        assignBtn(
          "Asignar",
          "Asignar el tag " + un[0].epc,
          un[0],
          "as:" + un[0].epc,
        ),
      ];
    } else {
      var b = h("button", "btn-warn", "Ver tags", {
        type: "button",
        "data-fk": "as:all",
        "aria-label": "Ver los " + n + " tags sin asignar",
      });
      b.addEventListener("click", () => {
        open.add("u");
        render("#u-first");
      });
      acts = [b];
    }
    return row({
      key: "u",
      cls: "attn wide",
      kan: n === 1 ? "Tag sin asignar" : n + " tags sin asignar",
      meta:
        n === 1
          ? [
              un[0].epc + " · " + (un[0].ant ? un[0].ant + " · " : ""),
              agoSpan(epcs),
            ]
          : [
              uniq(un.map((c) => c.ant).filter(Boolean)).join(", ") +
                " · última lectura ",
              agoSpan(epcs),
            ],
      acts: acts,
      detail:
        n === 1
          ? [
              h(
                "p",
                "desc",
                "Este tag todavía no está vinculado a ningún kanban.",
              ),
              h(
                "p",
                "why",
                "No se puede registrar un movimiento hasta asignarlo.",
              ),
            ]
          : [
              h(
                "p",
                "desc",
                "Estos tags todavía no están vinculados a ningún kanban. No se puede registrar un movimiento hasta asignarlos.",
              ),
              sub,
            ],
    });
  }
  // Resets the stuck confirmations so the gateway sends them again.
  async function requeueStuck() {
    if (busy) return;
    if (!(await ensureAuth())) return;
    busy = true;
    render();
    var r = null;
    try {
      r = await post("/api/confirmations/requeue", {});
    } catch (e) {}
    busy = false;
    if (r && r.status === 401) {
      render();
      onUnauthorized();
      return;
    }
    if (r && r.ok) {
      var n = r.data.requeued || 0;
      say(
        n === 1
          ? "1 confirmación volverá a enviarse."
          : n + " confirmaciones volverán a enviarse.",
      );
    } else {
      say("No se pudo reintentar. Revisa la conexión e intenta de nuevo.");
    }
    loadStatus();
  }
  function stuckRow(n) {
    var retry = h("button", "btn-warn", "Reintentar", {
      type: "button",
      "data-confirm": "1",
      "data-fk": "rq:stuck",
      "aria-label": "Reintentar el envío de las confirmaciones sin enviar",
    });
    retry.addEventListener("click", requeueStuck);
    return row({
      key: "stuck",
      cls: "attn wide",
      kan:
        n === 1
          ? "1 confirmación sin enviar"
          : n + " confirmaciones sin enviar",
      meta: ["No se reenvían solas · siguen guardadas en el gateway"],
      acts: [retry],
      detail: [
        h(
          "p",
          "desc",
          "El VPS rechazó estas confirmaciones (por ejemplo, tag o herramienta no registrados) o quedaron trabadas en una versión anterior del gateway. No se reenvían solas; corrige el dato en el VPS si hace falta y usa Reintentar. Los cortes de red ya no las traban: esas se reenvían solas.",
        ),
        h(
          "p",
          "why",
          "No se perdió nada: siguen guardadas en este gateway. Si siguen sin enviarse, avisa a soporte.",
        ),
      ],
    });
  }
  function doneSlip(cs) {
    var c = cs[0];
    var i = info(c);
    var n = cs.length;
    var li = h("li", "slip done");
    var d = h("div", "doneline");
    d.append(
      h(
        "span",
        c.offline ? "t-warn" : "t-ok",
        "✓ " + MOV[c.mov].noun + " confirmada",
      ),
      h("b", "", n > 1 ? baseOf(i.code) + " ×" + n : i.code),
    );
    var q = sumQty(cs);
    if (q) d.append(h("span", "", (qtyText(q) + " " + i.unit).trim()));
    d.append(
      h(
        "span",
        "chip" + (c.offline ? " warn" : ""),
        c.offline ? SYNC_PENDING : SYNC_OK,
      ),
    );
    li.append(d);
    return li;
  }
  // Group cards of the same kanban base (same unit, same pending/done status).
  function groupItems(items) {
    var map = new Map();
    var out = [];
    items.forEach((c) => {
      var i = info(c);
      var done = c.status === "done";
      var k = baseOf(i.code) + "|" + i.unit + "|" + done;
      var g = map.get(k);
      if (!g) {
        g = { base: baseOf(i.code), cs: [], done: done };
        map.set(k, g);
        out.push(g);
      }
      g.cs.push(c);
    });
    return out;
  }
  function inLane(mov) {
    return Array.from(cards.values()).filter(
      (c) => !c.unassigned && c.status !== "fail" && c.mov === mov,
    );
  }
  function lane(mov) {
    var m = MOV[mov];
    var items = inLane(mov);
    var pend = items.filter((c) => c.status !== "done");
    var compact = items.length > COMPACT_AT;
    wideCols = single && compact;
    var sec = h(
      "section",
      "lane " + m.cls + (compact ? " compact" : "") + (single ? " solo" : ""),
      undefined,
      { "aria-labelledby": "h-" + mov },
    );
    var top = h("div", "lane-top");
    var head = h("div", "lane-h");
    var l = h("div");
    l.append(
      h(
        "h3",
        "",
        m.arrow +
          (mov === "entrada" ? " Entradas al almacén" : " Salidas del almacén"),
        {
          id: "h-" + mov,
          tabindex: "-1",
        },
      ),
      h(
        "p",
        "",
        mov === "entrada"
          ? "Kanbans que llegan desde las extrusoras"
          : "Kanbans que salen del almacén",
      ),
    );
    var tot = h("div", "tot");
    tot.append(
      h("b", "", String(pend.length)),
      h("span", "", pend.length ? totals(pend) : "al día"),
    );
    head.append(l, tot);
    top.append(head);
    if (pend.length) {
      var label =
        progress && progress.mov === mov
          ? "Confirmando " +
            Math.min(progress.n + 1, progress.total) +
            " de " +
            progress.total +
            "…"
          : pend.length === 1
            ? "Confirmar la " + mov
            : "Confirmar las " + pend.length + " " + m.plural;
      var b = h(
        "button",
        "batch " + (m.cls === "in" ? "btn-in" : "btn-out"),
        label,
        {
          type: "button",
          "data-confirm": "1",
          "data-fk": "batch:" + mov,
        },
      );
      b.addEventListener("click", () => {
        runConfirm(pend, mov, "#h-" + mov);
      });
      top.append(b);
    }
    sec.append(top);
    if (!items.length) {
      sec.append(h("p", "lane-empty", "Sin " + m.plural + " por confirmar."));
      return sec;
    }
    var sc = h("div", "lane-scroll");
    sc.dataset.lane = mov;
    // Inner scroll only on wide screens; stacked lanes use the page scroll.
    if (!narrow.matches) {
      sc.tabIndex = 0;
      sc.setAttribute("role", "region");
      sc.setAttribute(
        "aria-label",
        (mov === "entrada" ? "Entradas" : "Salidas") +
          " por confirmar (lista con desplazamiento)",
      );
    }
    var ul = h("ul", "lane-list" + (single && !compact ? " grid" : ""));
    groupItems(items).forEach((g) => {
      if (g.cs.length > 1)
        ul.append(g.done ? doneSlip(g.cs) : groupRow(g, mov));
      else if (g.done) ul.append(doneSlip(g.cs));
      else ul.append(compact ? oneRow(g.cs[0]) : slip(g.cs[0]));
    });
    if (single && compact) sc.append(colHead());
    sc.append(ul);
    sec.append(sc);
    return sec;
  }
  function attentionItems() {
    var all = Array.from(cards.values());
    return {
      fails: all.filter((c) => c.status === "fail"),
      un: all.filter((c) => c.unassigned && c.status !== "fail"),
      stuck: status && status.stuck_count > 0 ? status.stuck_count : 0,
    };
  }
  function attentionCount(a) {
    return a.fails.length + a.un.length + (a.stuck ? 1 : 0);
  }
  function attnZone(a) {
    var z = h("section", "attn-zone", undefined, {
      "aria-labelledby": "attn-h",
    });
    z.append(
      h("h3", "", "⚠ Requieren tu atención (" + attentionCount(a) + ")", {
        id: "attn-h",
      }),
    );
    var ul = h("ul", "rows");
    a.fails.forEach((c) => {
      ul.append(oneRow(c));
    });
    if (a.stuck) ul.append(stuckRow(a.stuck));
    if (a.un.length) ul.append(unRow(a.un));
    z.append(ul);
    return z;
  }

  // ── render ─────────────────────────────────────────────────
  function render(focusSel) {
    var feed = $("feed");
    // Re-rendering must not throw the operator back to the top of a lane
    // or drop the focused control.
    var scroll = {};
    feed.querySelectorAll(".lane-scroll").forEach((e) => {
      scroll[e.dataset.lane] = e.scrollTop;
    });
    var active = document.activeElement;
    var fk = active && feed.contains(active) ? active.dataset.fk : "";

    feed.replaceChildren();
    wideCols = false;
    var a = attentionItems();
    if (attentionCount(a)) feed.append(attnZone(a));
    single = filter !== "all";
    var lanes = h("div", "lanes" + (single ? " single" : ""));
    if (!single || filter === "entrada") lanes.append(lane("entrada"));
    if (!single || filter === "salida") lanes.append(lane("salida"));
    feed.append(lanes);
    feed.querySelectorAll(".lane-scroll").forEach((e) => {
      e.scrollTop = scroll[e.dataset.lane] || 0;
    });
    cards.forEach((c) => {
      c.fresh = false;
    });

    feed.querySelectorAll("[data-confirm]").forEach((b) => {
      if (busy) b.disabled = true;
      else if (!canWrite()) {
        b.disabled = true;
        b.title = "Ingresa el token para confirmar";
      }
    });

    $("empty").hidden = !(cards.size === 0 && !a.stuck);
    feed.hidden = !$("empty").hidden;
    syncFilter();
    updateNote();

    var target = null;
    if (focusSel === "#u-first") target = feed.querySelector(".sub [data-fk]");
    else if (focusSel)
      target = feed.querySelector(focusSel) || feed.querySelector(".lane h3");
    else if (fk) target = feed.querySelector('[data-fk="' + fk + '"]');
    if (target && !target.disabled) target.focus();
  }

  function pendingIn(mov) {
    var n = 0;
    cards.forEach((c) => {
      if (c.unassigned || c.status !== "pending") return;
      if (!mov || c.mov === mov) n++;
    });
    return n;
  }
  // Counts cover only what the lanes hold; the attention zone is never filtered.
  function syncFilter() {
    var counts = {
      all: pendingIn(),
      entrada: pendingIn("entrada"),
      salida: pendingIn("salida"),
    };
    document.querySelectorAll("#f-seg button").forEach((b) => {
      var f = b.dataset.f;
      b.setAttribute("aria-pressed", String(f === filter));
      b.setAttribute(
        "aria-label",
        LABEL[f] + ": " + counts[f] + " por confirmar",
      );
      b.querySelector(".n").textContent = String(counts[f]);
    });
  }
  function updateNote() {
    var a = attentionItems();
    var att = attentionCount(a);
    var text =
      pendingIn() +
      " por confirmar" +
      (att
        ? " · " + att + (att === 1 ? " requiere" : " requieren") + " atención"
        : "") +
      (sseUp ? " · en vivo" : " · sin conexión en vivo");
    $("count-note").textContent = text;
  }
  function setFilter(f, focusSel) {
    filter = f;
    render(focusSel);
  }
  function hideToast() {
    clearTimeout(toastTimer);
    $("toast").hidden = true;
  }
  function announceMove(c) {
    var to = LABEL[c.mov];
    var msg = info(c).code + " pasó a " + to;
    say(msg);
    $("toast-msg").textContent = msg;
    var go = $("toast-go");
    go.textContent = "Ver en " + to;
    go.onclick = () => {
      hideToast();
      setFilter(c.mov, '[data-epc="' + c.epc + '"] .go');
    };
    $("toast").hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(hideToast, 8000);
  }

  // ── status summary ─────────────────────────────────────────
  function dot(id, cls) {
    $(id).className = "status-dot " + cls;
  }
  function cell(id, shown, dotCls, val, valCls, sub) {
    $("c-" + id).hidden = !shown;
    if (!shown) return;
    dot("d-" + id, dotCls);
    var v = $("v-" + id);
    v.textContent = val;
    v.className =
      "val" + (id === "q" ? " mono" : "") + (valCls ? " " + valCls : "");
    var s = $("s-" + id);
    s.hidden = !sub;
    if (sub) setNodes(s, [].concat(sub));
  }
  function renderStatus() {
    var st = status;
    var ants = antennas || [];
    var active = ants.filter((a) => a.state !== "stopped");
    var reading = active.filter((a) => a.state === "reading");
    var reconnecting = active.filter((a) => a.state === "reconnecting");
    var lastRead = null;
    ants.forEach((a) => {
      var t = parseTs(a.last_read_at);
      if (t !== null && (lastRead === null || t > lastRead)) lastRead = t;
    });

    // Antennas
    if (antennas && ants.length) {
      var sub = [];
      if (reconnecting.length)
        sub.push(reconnecting.map((a) => a.id).join(", ") + " reconectando…");
      else if (lastRead !== null)
        sub.push("Última lectura ", agoSpan(null, lastRead));
      cell(
        "ant",
        true,
        reconnecting.length || !active.length ? "" : "online",
        active.length
          ? reading.length + " de " + active.length + " leyendo"
          : "Sin antenas activas",
        "",
        sub.length ? sub : null,
      );
    } else cell("ant", false);

    // VPS
    if (st) {
      cell(
        "vps",
        true,
        st.vps_online ? "online" : "offline",
        st.vps_online ? "En línea" : "Sin conexión",
        st.vps_online ? "" : "t-bad",
        st.vps_online
          ? "Las confirmaciones se envían al instante"
          : st.last_sync_at
            ? "Último envío a las " + hhmm(Date.parse(st.last_sync_at))
            : null,
      );
      // Queue
      var oldest = parseTs(st.oldest_pending_at);
      cell(
        "q",
        true,
        st.pending_warning ||
          st.stuck_count > 0 ||
          (!st.vps_online && st.pending_count > 0)
          ? ""
          : "online",
        String(st.pending_count),
        "",
        oldest === null
          ? st.pending_count === 0
            ? "Todo sincronizado"
            : null
          : ["La más antigua: ", agoSpan(null, oldest)],
      );
    } else {
      cell("vps", false);
      cell("q", false);
    }

    // Access
    var token = Boolean(getToken());
    if (requiresBearer) {
      cell(
        "acc",
        true,
        token ? "online" : "",
        token ? "LAN · con token" : "LAN · falta token",
        token ? "" : "t-warn",
        token
          ? "Token guardado en esta pestaña"
          : "Puedes ver lecturas, pero no confirmar",
      );
    } else cell("acc", true, "online", "Local", "", "No requiere token");

    // Antenna list + queue detail
    $("sec-ant").hidden = !(antennas && ants.length);
    var list = $("ant-list");
    list.replaceChildren();
    ants.forEach((a) => {
      var li = h("li");
      var d = h(
        "span",
        "status-dot" +
          (a.state === "reading"
            ? " online"
            : a.state === "stopped"
              ? " offline"
              : ""),
      );
      var body = h("div");
      body.append(h("b", "", a.id));
      var words = [];
      if (a.zone) words.push("Zona: " + a.zone);
      words.push(
        a.state === "reading"
          ? "leyendo"
          : a.state === "reconnecting"
            ? "reconectando"
            : "detenida",
      );
      var sub = h("div", "sub");
      sub.append(words.join(" · "));
      var t = parseTs(a.last_read_at);
      if (t !== null) sub.append(" · ", agoSpan(null, t));
      body.append(sub);
      li.append(d, body);
      list.append(li);
    });
    $("sec-q").hidden = !st;
    if (st) {
      var big = $("q-big");
      big.textContent = String(st.pending_count);
      big.className = "q-count" + (st.pending_warning ? " warn" : "");
      var kv = $("q-kv");
      kv.replaceChildren();
      function line(k, v) {
        kv.append(h("dt", "", k), h("dd", "", v));
      }
      if (kanbans && kanbans.length)
        line("Kanbans en catálogo", String(kanbans.length));
      if (st.pending_warning_threshold > 0)
        line("Límite de aviso", String(st.pending_warning_threshold));
      if (st.max_pending_confirmations > 0)
        line("Límite de la cola", String(st.max_pending_confirmations));
      if (st.stuck_count > 0)
        line("Sin enviar (requieren Reintentar)", String(st.stuck_count));
      if (st.last_sync_at)
        line("Último envío al VPS", hhmm(Date.parse(st.last_sync_at), true));
    }

    // One-line summary; opens itself when the set of issues changes.
    var issues = [];
    var bad = false;
    if (statusErr) {
      issues.push("No se pudo consultar el estado del gateway");
      bad = true;
    }
    if (st && !st.vps_online) {
      issues.push("Sin conexión con el VPS");
      bad = true;
    }
    if (reconnecting.length)
      issues.push(reconnecting.map((a) => a.id).join(", ") + " reconectando");
    if (requiresBearer && !token) issues.push("Falta token para confirmar");
    if (st && st.pending_warning)
      issues.push("Cola por sincronizar alta (" + st.pending_count + ")");
    if (st && st.stuck_count > 0)
      issues.push(st.stuck_count + " sin enviar, requieren Reintentar");
    var issue = issues.join(" · ");
    var loaded = st || antennas || statusErr;
    dot("d-sum", loaded ? (issue ? (bad ? "offline" : "") : "online") : "");
    var sv = $("v-sum");
    sv.textContent = loaded ? issue || "Todo en orden" : "Conectando…";
    sv.className = "sum-val" + (issue ? (bad ? " t-bad" : " t-warn") : "");
    if (issue && issue !== lastIssue) $("sys").open = true;
    lastIssue = issue;
    $("b-vps").hidden = !(st && !st.vps_online);
  }

  // ── history ────────────────────────────────────────────────
  function renderHistory() {
    var box = $("hist");
    if (!history || !history.length) {
      box.hidden = true;
      return;
    }
    box.hidden = false;
    $("hist-count").textContent =
      history.length + (history.length === 1 ? " confirmada" : " confirmadas");
    var list = $("hist-list");
    list.replaceChildren();
    history.forEach((d) => {
      var mov = MOV[d.action] || MOV.entrada;
      var li = h("li", "hrow");
      var l = h("div");
      var title = d.kanban_code || "Tag " + d.uii;
      l.append(h("b", "", title + (d.description ? " " : "")));
      if (d.description) l.append(d.description);
      var bits = [];
      if (typeof d.quantity === "number")
        bits.push((qtyText(d.quantity) + " " + (d.unit || "")).trim());
      if (d.antenna_id) bits.push(d.antenna_id);
      bits.push(hhmm(Date.parse(d.time)));
      l.append(h("div", "meta", bits.join(" · ")));
      var r = h("div", "res");
      r.append(
        h(
          "span",
          d.synced ? "t-ok" : "t-warn",
          "✓ " + mov.noun + " confirmada",
        ),
        h(
          "span",
          "chip" + (d.synced ? "" : " warn"),
          d.synced ? SYNC_OK : SYNC_PENDING,
        ),
      );
      li.append(l, r);
      list.append(li);
    });
  }

  // ── data loading ───────────────────────────────────────────
  async function getJSON(url) {
    var r = await fetch(url);
    if (!r.ok) throw new Error(url + " " + r.status);
    return r.json();
  }
  async function loadStatus() {
    try {
      status = await getJSON("/api/status");
      statusErr = false;
    } catch (e) {
      statusErr = true;
    }
    renderStatus();
    // The attention zone depends on stuck_count.
    render();
  }
  async function loadAntennas() {
    try {
      antennas = await getJSON("/api/antennas");
    } catch (e) {
      antennas = null;
    }
    renderStatus();
  }
  async function loadHistory() {
    try {
      history = await getJSON("/api/history");
    } catch (e) {
      history = null;
    }
    renderHistory();
  }
  async function loadKanbans() {
    try {
      var k = await getJSON("/api/kanbans");
      kanbans = Array.isArray(k) ? k : [];
    } catch (e) {
      if (!kanbans) kanbans = null;
    }
    return kanbans;
  }
  // Merge the unassigned inbox so a reload (or a missed SSE event) still shows
  // the tags waiting for assignment.
  async function loadUnassigned() {
    var inbox;
    try {
      inbox = await getJSON("/api/unassigned-tags");
    } catch (e) {
      return;
    }
    if (!Array.isArray(inbox)) return;
    var changed = false;
    var seen = new Set();
    inbox.forEach((t) => {
      seen.add(t.epc);
      if (cards.has(t.epc)) return;
      var c = newCard(t.epc);
      c.ant = t.antenna_id || "";
      c.readAt = parseTs(t.last_seen) || Date.now();
      c.unassigned = true;
      cards.set(c.epc, c);
      changed = true;
    });
    cards.forEach((c, epc) => {
      if (
        c.unassigned &&
        c.status === "pending" &&
        !seen.has(epc) &&
        Date.now() - c.readAt > UNASSIGNED_GRACE_MS
      ) {
        cards.delete(epc);
        changed = true;
      }
    });
    if (changed) render();
  }
  function refresh() {
    loadStatus();
    loadAntennas();
    loadUnassigned();
    loadHistory();
  }

  // ── kanban picker ──────────────────────────────────────────
  var pick = { card: null, sel: null, opener: "" };
  function pickerMatches() {
    var q = $("picker-q").value.trim().toLowerCase();
    var all = kanbans || [];
    if (!q) return all;
    return all.filter(
      (k) =>
        (k.code || "").toLowerCase().indexOf(q) !== -1 ||
        (k.description || "").toLowerCase().indexOf(q) !== -1 ||
        (k.sku || "").toLowerCase().indexOf(q) !== -1,
    );
  }
  function syncPickerOk() {
    var ok = $("picker-ok");
    ok.disabled = !pick.sel;
    ok.textContent = pick.sel ? "Asignar a " + pick.sel.code : "Asignar";
    $("picker-list")
      .querySelectorAll("button")
      .forEach((b) => {
        b.setAttribute(
          "aria-pressed",
          String(Boolean(pick.sel) && b.dataset.code === pick.sel.code),
        );
      });
  }
  function renderPicker() {
    var list = $("picker-list");
    var note = $("picker-note");
    list.replaceChildren();
    if (kanbans === null) {
      note.textContent =
        "No se pudo cargar el catálogo de kanbans. Cierra e intenta de nuevo.";
      return;
    }
    if (!kanbans.length) {
      note.textContent =
        "El catálogo de kanbans está vacío. Se llena con la sincronización con el VPS.";
      return;
    }
    var m = pickerMatches();
    var q = $("picker-q").value.trim();
    if (!m.length) note.textContent = "Sin resultados para «" + q + "».";
    else if (m.length > PICKER_MAX)
      note.textContent =
        "Mostrando " +
        PICKER_MAX +
        " de " +
        m.length +
        ". Escribe para filtrar.";
    else
      note.textContent = m.length + (m.length === 1 ? " kanban" : " kanbans");
    m.slice(0, PICKER_MAX).forEach((k) => {
      var li = h("li");
      var b = h("button", "", undefined, {
        type: "button",
        "aria-pressed": "false",
      });
      b.dataset.code = k.code;
      b.append(h("span", "p-code", k.code));
      var d = [
        k.description,
        qtyText(k.standard_pack) &&
          (qtyText(k.standard_pack) + " " + (k.unit || "")).trim(),
      ]
        .filter(Boolean)
        .join(" · ");
      if (d) b.append(h("span", "p-desc", d));
      b.addEventListener("click", () => {
        pick.sel = k;
        syncPickerOk();
      });
      li.append(b);
      list.append(li);
    });
    syncPickerOk();
  }
  async function openPicker(c, opener) {
    if (busy) return;
    if (!(await ensureAuth())) return;
    pick = { card: c, sel: null, opener: opener };
    $("picker-epc").textContent = "Tag " + c.epc + (c.ant ? " · " + c.ant : "");
    $("picker-q").value = "";
    $("picker-err").hidden = true;
    $("picker-note").textContent = kanbans ? "" : "Cargando catálogo…";
    syncPickerOk();
    renderPicker();
    $("picker").showModal();
    $("picker-q").focus();
    // Refresh the cached catalog in the background; it may be stale.
    loadKanbans().then(renderPicker);
  }
  function closePicker() {
    var dlg = $("picker");
    if (dlg.open) dlg.close();
  }
  async function submitPicker(e) {
    e.preventDefault();
    if (!pick.sel || !pick.card) return;
    var c = pick.card;
    var ok = $("picker-ok");
    var err = $("picker-err");
    err.hidden = true;
    ok.disabled = true;
    ok.textContent = "Asignando…";
    var r;
    try {
      r = await post("/api/tags/assign", {
        uii: c.epc,
        kanban_code: pick.sel.code,
        antenna_id: c.ant,
      });
    } catch (x) {
      err.textContent =
        "Sin respuesta del gateway. Revisa la conexión e intenta de nuevo.";
      err.hidden = false;
      syncPickerOk();
      return;
    }
    if (r.status === 401) {
      closePicker();
      onUnauthorized();
      return;
    }
    if (!r.ok || r.data.success === false) {
      err.textContent =
        r.data.error || r.data.message || "No se pudo asignar la tag.";
      err.hidden = false;
      syncPickerOk();
      return;
    }
    // The tag leaves the attention zone and joins its lane, ready to confirm.
    c.kanban = r.data.kanban || { code: pick.sel.code };
    c.unassigned = false;
    c.status = "pending";
    c.failMsg = "";
    c.fresh = true;
    closePicker();
    say(r.data.message || "Tag asignada al kanban " + c.kanban.code + ".");
    render('[data-epc="' + c.epc + '"] .go');
    loadUnassigned();
    loadKanbans().then(renderStatus);
  }

  // ── theme ──────────────────────────────────────────────────
  function setTheme(t, save) {
    document.documentElement.dataset.theme = t;
    document.querySelectorAll("#theme-hdr [data-t]").forEach((b) => {
      b.setAttribute("aria-pressed", String(b.dataset.t === t));
    });
    if (save) {
      try {
        localStorage.setItem(THEME_KEY, t);
      } catch (e) {}
      say("Tema " + (t === "dark" ? "oscuro" : "claro"));
    }
  }

  // ── live feed ──────────────────────────────────────────────
  var es = null;
  function connectSSE() {
    es = new EventSource("/events");
    es.addEventListener("connected", () => {
      sseUp = true;
      updateNote();
    });
    es.addEventListener("heartbeat", () => {
      if (!sseUp) {
        sseUp = true;
        updateNote();
      }
    });
    es.addEventListener("tag_detected", (ev) => {
      var d;
      try {
        d = JSON.parse(ev.data);
      } catch (e) {
        return;
      }
      sseUp = true;
      if (onTag(d)) render();
    });
    es.onerror = () => {
      sseUp = false;
      updateNote();
      // The browser retries by itself unless the connection was closed for good.
      if (es.readyState === EventSource.CLOSED) setTimeout(connectSSE, 5000);
    };
  }

  // ── wiring ─────────────────────────────────────────────────
  document.querySelectorAll("#f-seg button").forEach((b) => {
    b.addEventListener("click", () => {
      setFilter(b.dataset.f);
    });
  });
  document.querySelectorAll("#theme-hdr [data-t]").forEach((b) => {
    b.addEventListener("click", () => {
      setTheme(b.dataset.t, true);
    });
  });
  $("token").addEventListener("submit", (e) => {
    e.preventDefault();
    var v = $("tok").value.trim();
    if (!v) return;
    setToken(v);
    $("tok").value = "";
    syncTokenForm();
    say("Token guardado.");
  });
  $("picker-q").addEventListener("input", renderPicker);
  $("picker-form").addEventListener("submit", submitPicker);
  $("picker-cancel").addEventListener("click", closePicker);
  $("picker").addEventListener("close", () => {
    var t = document.querySelector('[data-fk="' + pick.opener + '"]');
    if (t && !t.disabled) t.focus();
  });
  narrow.addEventListener("change", () => {
    render();
  });

  setTheme(document.documentElement.dataset.theme, false);
  tickAgo();
  setInterval(tickAgo, 1000);
  render();
  renderStatus();
  connectSSE();
  loadKanbans().then(renderStatus);
  refresh();
  setInterval(refresh, POLL_MS);
})();
