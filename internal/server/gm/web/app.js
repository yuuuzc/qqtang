"use strict";

const state = { accounts: [], selectedUIN: 0, account: null, progression: null, petCatalog: [], petNames: new Map(), petByType: new Map(), pendingGrantItem: null, inventoryPage: 1, inventoryPageSize: 12, inventoryKind: "", itemPage: 1, itemPageSize: 60, itemTotal: 0, itemAbort: null };
const $ = (id) => document.getElementById(id);
const itemMotionTimers = new WeakMap();

async function api(path, options = {}) {
  const response = await fetch(path, { ...options, headers: { "Content-Type": "application/json", ...(options.headers || {}) } });
  if (response.status === 204) return null;
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || `请求失败 (${response.status})`);
  return data;
}

function toast(message, isError = false) {
  const node = $("toast");
  node.textContent = message;
  node.className = `toast${isError ? " error" : ""}`;
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => node.classList.add("hidden"), 3200);
}

function itemName(id) {
  const row = document.querySelector(`[data-item-id="${id}"]`);
  return row?.dataset.itemName || `物品 ${id}`;
}

function selectedRole() {
  // The selected character is session state supplied by REQUEST_LOGIN, not an
  // account property. GM image previews use Maria as a stable neutral model;
  // per-character equipment continues to carry its own role_id.
  return 7;
}

function itemImageURL(id, role = selectedRole(), appearance = false, animate = false) {
  return `/gm/api/items/${id}/image?role=${role}&appearance=${appearance ? 1 : 0}&animate=${animate ? 1 : 0}`;
}

function refreshItemImages() {
	document.querySelectorAll("img[data-item-image]").forEach((image) => {
		const role = Number(image.dataset.imageRole) || selectedRole();
		const appearance = image.dataset.imageAppearance === "true";
		image.dataset.imageAnimated = "false";
		image.src = itemImageURL(Number(image.dataset.itemImage), role, appearance, false);
	});
	bindItemImageMotion();
}

function bindItemImageMotion() {
	document.querySelectorAll("img[data-item-image]:not([data-motion-bound])").forEach((image) => {
		image.dataset.motionBound = "1";
		image.dataset.imageAnimated = "false";
		const update = (animate) => {
			const next = animate ? "true" : "false";
			if (image.dataset.imageAnimated === next) return;
			image.dataset.imageAnimated = next;
			const role = Number(image.dataset.imageRole) || selectedRole();
			image.src = itemImageURL(Number(image.dataset.itemImage), role, image.dataset.imageAppearance === "true", animate);
		};
		const start = () => {
			clearTimeout(itemMotionTimers.get(image));
			itemMotionTimers.set(image, setTimeout(() => update(true), 140));
		};
		const stop = () => {
			clearTimeout(itemMotionTimers.get(image));
			itemMotionTimers.delete(image);
			update(false);
		};
		image.addEventListener("pointerenter", start);
		image.addEventListener("pointerleave", stop);
		image.addEventListener("focus", start);
		image.addEventListener("blur", stop);
	});
}

async function loadHealth() {
  const node = $("health");
  try {
    const health = await api("/gm/api/health");
    node.className = "health ok";
    node.innerHTML = `<span></span>服务正常 · ${health.item_count} 项物品 · ${health.pet_type_count} 种宠物`;
  } catch (error) {
    node.className = "health error";
    node.innerHTML = `<span></span>服务不可用`;
  }
}

async function loadProgression() {
  state.progression = await api("/gm/api/progression");
  $("profile-identity").innerHTML = state.progression.identities.map((entry) =>
    `<option value="${entry.value}">${escapeHTML(entry.label)}（${entry.value}）</option>`).join("");
  $("profile-degree").innerHTML = state.progression.competitive_ranks.map((rank) =>
    `<option value="${rank.degree}">Lv.${rank.degree} · ${escapeHTML(rank.title)} · 第${rank.sub_level}阶</option>`).join("");
  $("profile-adventure-level").innerHTML = state.progression.adventure_ranks.map((rank) =>
    `<option value="${rank.level}">探险 Lv.${rank.level}</option>`).join("");
}

function competitiveRankForPoints(points) {
  const ranks = state.progression?.competitive_ranks || [];
  return ranks.find((rank) => points >= rank.min_points && points <= rank.max_points) || ranks[ranks.length - 1];
}

function adventureRankForPoints(points) {
  const ranks = state.progression?.adventure_ranks || [];
  return ranks.find((rank) => points >= rank.min_points && points <= rank.max_points) || ranks[ranks.length - 1];
}

function showCompetitiveRank(rank) {
  if (!rank) return;
  $("profile-degree").value = rank.degree;
  $("profile-points").min = rank.min_points;
  $("profile-points").max = rank.max_points;
  $("profile-degree-detail").textContent = `${rank.title} · 主徽章 ${rank.main_level} · 子图标 ${rank.sub_level}/6`;
  $("profile-points-range").textContent = `本级范围：${rank.min_points.toLocaleString()}–${rank.max_points.toLocaleString()}`;
}

function showAdventureRank(rank) {
  if (!rank) return;
  $("profile-adventure-level").value = rank.level;
  $("profile-adventure-points").min = rank.min_points;
  $("profile-adventure-points").max = rank.max_points;
  $("profile-adventure-range").textContent = `本级范围：${rank.min_points.toLocaleString()}–${rank.max_points.toLocaleString()}`;
}

function selectCompetitiveRank() {
  const rank = state.progression.competitive_ranks.find((entry) => entry.degree === numberValue("profile-degree"));
  if (!rank) return;
  setValue("profile-points", rank.min_points);
  showCompetitiveRank(rank);
}

function deriveCompetitiveRank() {
  showCompetitiveRank(competitiveRankForPoints(numberValue("profile-points")));
}

function selectAdventureRank() {
  const rank = state.progression.adventure_ranks.find((entry) => entry.level === numberValue("profile-adventure-level"));
  if (!rank) return;
  setValue("profile-adventure-points", rank.min_points);
  showAdventureRank(rank);
}

function deriveAdventureRank() {
  showAdventureRank(adventureRankForPoints(numberValue("profile-adventure-points")));
}

async function loadAccounts(selectUIN = state.selectedUIN) {
  const data = await api("/gm/api/accounts");
  state.accounts = data.accounts || [];
  renderAccounts();
  if (selectUIN && state.accounts.some((entry) => entry.uin === selectUIN)) await selectAccount(selectUIN);
  else if (state.accounts.length && !state.selectedUIN) await selectAccount(state.accounts[0].uin);
  else if (!state.accounts.length) showEmpty();
}

function renderAccounts() {
  const query = $("account-search").value.trim().toLowerCase();
  const filtered = state.accounts.filter((account) => `${account.uin} ${account.nickname}`.toLowerCase().includes(query));
  $("account-list").innerHTML = filtered.length ? filtered.map((account) => `
    <button class="account-card ${account.uin === state.selectedUIN ? "active" : ""}" data-uin="${account.uin}">
      <span class="avatar">${escapeHTML((account.nickname || "?").slice(0, 1))}</span>
      <span><strong>${escapeHTML(account.nickname || "未命名")}</strong><small>${account.uin} · ${account.inventory_count} 种物品</small></span>
      <span class="level">Lv.${account.degree}</span>
    </button>`).join("") : `<p class="hint">没有匹配的本地账号。</p>`;
  document.querySelectorAll(".account-card").forEach((button) => button.addEventListener("click", () => selectAccount(Number(button.dataset.uin))));
}

async function selectAccount(uin) {
	const accountChanged = state.selectedUIN !== uin;
  state.selectedUIN = uin;
	if (accountChanged) {
		state.inventoryPage = 1;
		state.inventoryKind = "";
	}
  state.account = await api(`/gm/api/accounts/${uin}`);
  $("empty-state").classList.add("hidden");
  $("account-workspace").classList.remove("hidden");
  renderAccounts();
  renderProfile();
  renderInventory();
  await loadPets();
  refreshItemImages();
  await loadItems(true);
}

function showEmpty() {
  state.selectedUIN = 0;
  state.account = null;
  $("empty-state").classList.remove("hidden");
  $("account-workspace").classList.add("hidden");
}

function renderProfile() {
  const { uin, profile } = state.account;
  const game = profile.game_info;
  $("profile-title").textContent = `${profile.nickname} · ${uin}`;
  setValue("profile-uin", uin); setValue("profile-player-id", profile.player_id); setValue("profile-nickname", profile.nickname);
  setValue("profile-gender", profile.gender); setValue("profile-degree", game.degree);
  setValue("profile-points", game.points); setValue("profile-adventure-points", game.extended_points); setValue("profile-money", game.money);
  setValue("profile-identity", profile.identity); setValue("profile-wins", game.wins); setValue("profile-losses", game.losses); setValue("profile-draws", game.draws);
  setValue("profile-password", "");
  $("profile-tutorial").checked = Boolean(profile.tutorial_completed);
  deriveCompetitiveRank();
  if (game.degree !== numberValue("profile-degree")) {
    $("profile-degree-detail").textContent += ` · 存档字段 Lv.${game.degree} 与积分不一致，保存后将统一`;
  }
  deriveAdventureRank();
}

function renderInventory() {
	const inventory = state.account.profile.inventory || [];
	const metadataByID = new Map((state.account.inventory_items || []).map((item) => [item.id, item]));
	const kinds = [...new Set(inventory.map((item) => metadataByID.get(item.id)?.kind || "unknown"))]
		.sort((left, right) => kindLabel(left).localeCompare(kindLabel(right), "zh-CN"));
	if (state.inventoryKind && !kinds.includes(state.inventoryKind)) state.inventoryKind = "";
	$("inventory-kind").innerHTML = `<option value="">全部分类</option>${kinds.map((kind) => `<option value="${escapeHTML(kind)}">${escapeHTML(kindLabel(kind))}</option>`).join("")}`;
	$("inventory-kind").value = state.inventoryKind;
	const filtered = inventory.filter((item) => !state.inventoryKind || (metadataByID.get(item.id)?.kind || "unknown") === state.inventoryKind);
	const pageCount = Math.max(1, Math.ceil(filtered.length / state.inventoryPageSize));
	state.inventoryPage = Math.min(Math.max(1, state.inventoryPage), pageCount);
	const start = (state.inventoryPage - 1) * state.inventoryPageSize;
	const visible = filtered.slice(start, start + state.inventoryPageSize);
	$("inventory-count").textContent = state.inventoryKind ? `${filtered.length} / ${inventory.length}` : inventory.length;
	$("inventory-page").textContent = `第 ${state.inventoryPage} / ${pageCount} 页`;
	$("inventory-prev").disabled = state.inventoryPage <= 1;
	$("inventory-next").disabled = state.inventoryPage >= pageCount;
	$("inventory-list").innerHTML = visible.length ? visible.map((item) => {
		const metadata = metadataByID.get(item.id);
		const equipped = (state.account.loadouts || []).find((entry) => entry.item_id === item.id);
		const imageRole = equipped?.role_id || selectedRole();
		return `
		<div class="inventory-row" data-item-id="${item.id}" data-item-name="${escapeHTML(metadata?.name || `物品 ${item.id}`)}">
			<img class="item-icon" tabindex="0" decoding="async" data-item-image="${item.id}" data-image-role="${imageRole}" data-image-appearance="${Boolean(equipped)}" src="${itemImageURL(item.id, imageRole, Boolean(equipped), false)}" alt="${escapeHTML(metadata?.name || `物品 ${item.id}`)}">
			<div class="item-copy"><strong>${escapeHTML(metadata?.name || `物品 ${item.id}`)}</strong><small>ID ${item.id} · ${escapeHTML(kindLabel(metadata?.kind || "unknown"))}${loadoutText(item.id)}</small><span class="inventory-item-description${metadata?.description ? "" : " hidden"}">${escapeHTML(metadata?.description || "")}</span></div>
			<div class="quantity-editor"><input type="number" min="1" max="999" value="${item.quantity}" aria-label="数量"><button class="secondary save-item">更新</button></div>
			<button class="danger remove-item">移除</button>
		</div>`;
	}).join("") : `<p class="hint">${inventory.length ? "此分类没有物品。" : "背包为空。请从下方物品目录添加。"}</p>`;
  document.querySelectorAll(".inventory-row").forEach((row) => {
    row.querySelector(".save-item").addEventListener("click", () => updateInventory(Number(row.dataset.itemId), Number(row.querySelector("input").value)).catch(reportError));
    row.querySelector(".remove-item").addEventListener("click", () => removeInventory(Number(row.dataset.itemId)).catch(reportError));
  });
	bindItemImageMotion();
}

function loadoutText(itemID) {
  const matches = (state.account.loadouts || []).filter((entry) => entry.item_id === itemID);
  return matches.length ? ` · 已装备：${matches.map((entry) => `角色${entry.role_id}（${roleDisplayName(entry.role_id)}）/${slotDisplayName(entry.slot)}`).join("、")}` : "";
}

function roleDisplayName(roleID) {
  // IDs and short resource names are taken from uiRoom.pyc RoleIDList /
  // RoleNameList. Keep unknown future roles readable without guessing.
  return ({ 1:"小悟空", 2:"泰坦", 3:"小倩", 4:"春丽", 5:"波波利", 6:"火影", 7:"玛丽亚", 8:"海王子", 9:"毛毛", 13:"可乐", 14:"哪吒", 15:"乌拉拉", 16:"丫丫", 22:"阿莎", 23:"随机角色" })[roleID] || "未知角色";
}

function slotDisplayName(slot) {
  return ({ cap:"帽子", hair:"头发", eye:"眼部", face:"面部", mouth:"嘴部", cloth:"服装", cloth_adornment:"服装饰品", front_pack:"前挂饰", back_pack:"背饰", ear:"耳饰", bubble:"糖泡", footprint:"脚印", background:"背景", frame:"资料边框", enter:"入场效果", namecard:"名片", namecard_bound:"名片边框" })[slot] || slot;
}

async function saveProfile() {
  if (!$("profile-form").reportValidity()) return;
  const payload = {
    nickname: $("profile-nickname").value.trim(), gender: numberValue("profile-gender"),
    degree: numberValue("profile-degree"), points: numberValue("profile-points"), adventure_points: numberValue("profile-adventure-points"),
    money: numberValue("profile-money"), identity: numberValue("profile-identity"), wins: numberValue("profile-wins"),
    losses: numberValue("profile-losses"), draws: numberValue("profile-draws"), tutorial_completed: $("profile-tutorial").checked
  };
  state.account = await api(`/gm/api/accounts/${state.selectedUIN}`, { method: "PUT", body: JSON.stringify(payload) });
  const password = $("profile-password").value;
  if (password) {
    state.account = await api(`/gm/api/accounts/${state.selectedUIN}/password`, { method: "PUT", body: JSON.stringify({ password }) });
    setValue("profile-password", "");
  }
  toast("人物资料已保存；重新登录客户端后生效。");
  await loadAccounts(state.selectedUIN);
}

async function updateInventory(itemID, quantity) {
  if (!Number.isInteger(quantity) || quantity < 1 || quantity > 999) return toast("道具数量必须在 1–999 之间；删除请使用“移除”按钮。", true);
  state.account = await api(`/gm/api/accounts/${state.selectedUIN}/inventory/${itemID}`, { method: "PUT", body: JSON.stringify({ quantity }) });
  renderInventory();
  toast(`已更新物品 ${itemID} 数量。`);
  await loadAccounts(state.selectedUIN);
}

async function removeInventory(itemID) {
  if (!confirm(`确定从背包移除 ${itemName(itemID)}（ID ${itemID}）？相关角色装备槽也会清除。`)) return;
  state.account = await api(`/gm/api/accounts/${state.selectedUIN}/inventory/${itemID}`, { method: "DELETE" });
  renderInventory();
  toast(`已移除物品 ${itemID}。`);
  await loadAccounts(state.selectedUIN);
}

async function loadPets() {
  const data = await api(`/gm/api/pets?role=${selectedRole()}&limit=100`);
  state.petCatalog = data.pets || [];
  state.petNames.clear();
  state.petByType.clear();
  state.petCatalog.forEach((pet) => {
    if (pet.pet_type_id) {
      state.petNames.set(pet.pet_type_id, pet.name);
      state.petByType.set(pet.pet_type_id, pet);
    }
  });
  renderOwnedPets();
  renderPetCatalog();
}

function renderOwnedPets() {
  const pets = state.account?.pets || [];
  $("pet-count").textContent = pets.length;
  $("owned-pet-list").innerHTML = pets.length ? pets.map((pet) => {
    const metadata = state.petByType.get(pet.pet_type_id);
    const mark = petImageMarkup(metadata, pet.pet_type_id);
    return `
    <article class="owned-pet" data-pet-id="${pet.pet_id}">
      ${mark}
      <span><strong>${escapeHTML(metadata?.name || pet.name || `宠物 ${pet.pet_type_id}`)}</strong><small>${metadata?.card_item_id ? `宠物卡 ${metadata.card_item_id} · ` : ""}实体类型 ${pet.pet_type_id} · 实例 ${pet.pet_id} · Lv.${pet.level} · ${pet.state === 1 ? "携带中" : "未携带"}</small></span>
      <button class="danger remove-pet">移除</button>
    </article>`;
  }).join("") : `<p class="hint">尚未拥有宠物。</p>`;
  document.querySelectorAll(".remove-pet").forEach((button) => button.addEventListener("click", () => {
    const petID = Number(button.closest(".owned-pet").dataset.petId);
    removePet(petID).catch(reportError);
  }));
  bindItemImageMotion();
}

function renderPetCatalog() {
  const ownedTypes = new Set((state.account?.pets || []).map((pet) => pet.pet_type_id));
  const query = $("pet-search").value.trim().toLowerCase();
  const filtered = state.petCatalog.filter((pet) => !query || `${pet.name} ${pet.type_name || ""} ${pet.description || ""} ${pet.pet_type_id || ""} ${pet.card_item_id || ""} ${pet.card_resource_id || ""} ${pet.level_4_resource_id || ""} ${pet.level_7_resource_id || ""}`.toLowerCase().includes(query));
  $("pet-total").textContent = `找到 ${filtered.length} / ${state.petCatalog.length} 种 · ${state.petCatalog.filter((pet) => pet.card_item_id).length} 种已关联原版宠物卡`;
  $("pet-grid").innerHTML = filtered.map((pet) => {
    const owned = pet.pet_type_id && ownedTypes.has(pet.pet_type_id);
    const status = pet.resource_status === "complete" ? "1/4/7级模型完整" : pet.resource_status === "starter-only" ? "仅初始通用模型；4/7级外观缺失" : "模型资源缺失";
    const modelText = pet.level_7_resource_id ? `${pet.level_4_resource_id}/${pet.level_7_resource_id}` : `${pet.level_4_resource_id || "—"}`;
    const disabled = !pet.assignable || owned;
    const mark = petImageMarkup(pet, pet.pet_type_id || modelText);
    const identity = pet.card_item_id ? `宠物卡 ${pet.card_item_id} / 资源 ${pet.card_resource_id} → 实体类型 ${pet.pet_type_id}` : pet.pet_type_id ? `实体类型 ${pet.pet_type_id} · 当前物品表无对应宠物卡` : "尚无 PetTypeID";
    return `<article class="pet-card${pet.assignable ? "" : " unmapped"}" title="${escapeHTML(pet.description || pet.reason || "PetCfg.ini 原始类型")}">
      ${mark}
      <div class="pet-card-copy"><strong>${escapeHTML(pet.name)}</strong><small title="${escapeHTML(identity)}">${escapeHTML(identity)} · 模型 ${modelText}</small><span class="pet-status ${pet.resource_status === "complete" ? "complete" : ""}">${status}</span><button class="${disabled ? "secondary" : "primary"} grant-pet" data-type-id="${pet.pet_type_id || 0}" data-name="${escapeHTML(pet.name)}" ${disabled ? "disabled" : ""}>${owned ? "已拥有" : pet.assignable ? "生成宠物" : "等待类型映射"}</button></div>
    </article>`;
  }).join("");
  document.querySelectorAll(".grant-pet:not(:disabled)").forEach((button) => button.addEventListener("click", () => grantPet(Number(button.dataset.typeId), button.dataset.name).catch(reportError)));
  bindItemImageMotion();
}

function petImageMarkup(pet, fallbackLabel) {
  if (pet?.card_item_id && pet.has_image) {
    return `<img class="item-icon" loading="lazy" decoding="async" data-item-image="${pet.card_item_id}" data-image-role="${selectedRole()}" data-image-appearance="false" src="${itemImageURL(pet.card_item_id, selectedRole(), false, false)}" alt="${escapeHTML(pet.name)}">`;
  }
  if (pet?.image_url && pet.has_image) {
    return `<img class="item-icon" loading="lazy" decoding="async" src="${escapeHTML(pet.image_url)}" alt="${escapeHTML(pet.name)}">`;
  }
  return `<span class="pet-mark">${escapeHTML(fallbackLabel)}</span>`;
}

async function grantPet(petTypeID, name) {
  if (!state.selectedUIN) return toast("请先选择账号。", true);
  state.account = await api(`/gm/api/accounts/${state.selectedUIN}/pets/${petTypeID}`, { method: "PUT" });
  await loadPets();
  toast(`已给 ${state.account.profile.nickname} 添加宠物 ${name}。重新登录后生效。`);
}

async function removePet(petID) {
  if (!confirm(`确定移除宠物实例 ${petID}？`)) return;
  state.account = await api(`/gm/api/accounts/${state.selectedUIN}/pet-instances/${petID}`, { method: "DELETE" });
  await loadPets();
  toast(`已移除宠物实例 ${petID}。`);
}

async function deleteAccount() {
  const account = state.account;
  if (!account || !confirm(`确定删除本地账号存档 ${account.profile.nickname}（${account.uin}）及其背包？\n\n在线账号不能删除，请先让对应客户端正常退出。此操作不会在普通服务端重启时自动重建账号。`)) return;
  await api(`/gm/api/accounts/${account.uin}`, { method: "DELETE" });
  state.selectedUIN = 0;
  toast(`已删除账号 ${account.uin}。`);
  await loadAccounts();
}

async function createAccount(event) {
  event.preventDefault();
  if (!$("create-form").reportValidity()) return;
  const payload = { uin: numberValue("create-uin"), nickname: $("create-nickname").value.trim(), gender: numberValue("create-gender"), password: $("create-password").value };
  const account = await api("/gm/api/accounts", { method: "POST", body: JSON.stringify(payload) });
  $("create-dialog").close();
  toast(`账号 ${account.uin} 已创建。`);
  await loadAccounts(account.uin);
}

function bindPasswordToggle(buttonID, inputID) {
  $(buttonID).addEventListener("click", () => {
    const input = $(inputID);
    const visible = input.type === "text";
    input.type = visible ? "password" : "text";
    $(buttonID).textContent = visible ? "显示" : "隐藏";
  });
}

let itemSearchTimer = 0;
async function loadItems(resetPage = true) {
  if (resetPage) state.itemPage = 1;
  if (state.itemAbort) state.itemAbort.abort();
  const controller = new AbortController();
  state.itemAbort = controller;
  setItemPaginationDisabled(true);
  const query = $("item-search").value.trim();
  const kind = $("item-kind").value;
  const requestedPage = state.itemPage;
  const offset = (requestedPage - 1) * state.itemPageSize;
  try {
    const images = $("item-images-only").checked ? "local" : "";
    const data = await api(`/gm/api/items?q=${encodeURIComponent(query)}&kind=${encodeURIComponent(kind)}&images=${images}&role=${selectedRole()}&offset=${offset}&limit=${state.itemPageSize}`, { signal: controller.signal });
    if (state.itemAbort !== controller) return;
    state.itemTotal = data.total;
    const pageCount = Math.max(1, Math.ceil(state.itemTotal / state.itemPageSize));
    state.itemPage = Math.min(requestedPage, pageCount);
    if ($("item-kind").options.length === 1) data.kinds.forEach((value) => $("item-kind").add(new Option(kindLabel(value), value)));
    const firstVisible = data.items.length ? offset + 1 : 0;
    const lastVisible = offset + data.items.length;
    $("item-total").textContent = `找到 ${data.total} 项 · 当前显示 ${firstVisible}–${lastVisible}`;
    $("item-page").textContent = `第 ${state.itemPage} / ${pageCount} 页`;
    $("item-grid").innerHTML = data.items.length ? data.items.map(itemCard).join("") : `<p class="hint">没有符合当前条件的物品。</p>`;
		document.querySelectorAll(".grant-item").forEach((button) => {
      button.addEventListener("click", () => grantItem(Number(button.dataset.id), button.dataset.name));
		});
		bindItemImageMotion();
    $("item-first").disabled = state.itemPage <= 1;
    $("item-prev").disabled = state.itemPage <= 1;
    $("item-next").disabled = state.itemPage >= pageCount;
    $("item-last").disabled = state.itemPage >= pageCount;
  } catch (error) {
    if (error.name !== "AbortError") throw error;
  } finally {
    if (state.itemAbort === controller) {
      state.itemAbort = null;
    }
  }
}

function setItemPaginationDisabled(disabled) {
  for (const id of ["item-first", "item-prev", "item-next", "item-last"]) $(id).disabled = disabled;
}

function changeItemPage(page) {
  const pageCount = Math.max(1, Math.ceil(state.itemTotal / state.itemPageSize));
  const nextPage = Math.min(Math.max(1, page), pageCount);
  if (nextPage === state.itemPage) return;
  state.itemPage = nextPage;
  loadItems(false).catch(reportError);
}

function itemCard(item) {
	const appearance = Boolean(item.has_appearance);
	const source = imageSourceLabel(appearance ? item.appearance_source : item.image_source);
	const catalogSource = imageSourceLabel(item.image_source);
	const description = String(item.description || "").trim();
	return `<article class="item-card${description ? " has-description" : ""}" data-item-id="${item.id}" data-item-name="${escapeHTML(item.name)}"${description ? ` aria-describedby="item-description-${item.id}"` : ""}>
		<img class="item-icon${item.has_image ? "" : " missing"}" tabindex="0" loading="lazy" decoding="async" data-item-image="${item.id}" data-image-role="${selectedRole()}" data-image-appearance="${appearance}" src="${itemImageURL(item.id, selectedRole(), appearance, false)}" alt="${escapeHTML(item.name)}">
		<div class="item-card-copy"><strong title="${escapeHTML(item.name)}">${escapeHTML(item.name)}</strong><small>ID ${item.id} · ${escapeHTML(kindLabel(item.kind))}${item.slot ? ` · ${escapeHTML(item.slot)}` : ""}</small><code class="resource-key" title="账号库存资源键；不与同号对局元素混用">${escapeHTML(item.resource_key)}</code><div class="item-badges"><span class="image-source ${item.has_image ? "" : "missing"}" title="目录图标资源：${escapeHTML(catalogSource)}">${escapeHTML(source)}</span>${item.scene_id_collision ? `<span class="namespace-note" title="客户端对局场景工厂也识别相同数字 ID；两者资源命名空间彼此独立">同号场景元素</span>` : ""}</div><div class="item-actions"><button class="primary grant-item" data-id="${item.id}" data-name="${escapeHTML(item.name)}">加入背包</button></div></div>
		${description ? `<div class="item-description-tooltip" id="item-description-${item.id}" role="tooltip">${escapeHTML(description)}</div>` : ""}
	</article>`;
}

function imageSourceLabel(value) {
  return ({ "item-icon":"原版物品图标", "category-icon":"原版分类图标", "appearance-layer":"当前角色原版装扮图层", "canonical-alias":"原版规范物品映射", "explicit-alias":"原版显式资源映射" })[value] || "当前客户端未含对应图片";
}

function grantItem(itemID, name) {
  if (!state.selectedUIN) return toast("请先选择账号。", true);
  state.pendingGrantItem = { itemID, name };
  $("grant-item-name").textContent = `${name}（ID ${itemID}）`;
  setValue("grant-item-quantity", 1);
  $("grant-item-dialog").showModal();
  $("grant-item-quantity").focus();
  $("grant-item-quantity").select();
}

async function confirmGrantItem(event) {
  event.preventDefault();
  const pending = state.pendingGrantItem;
  if (!pending || !state.selectedUIN) return;
  const quantity = numberValue("grant-item-quantity");
  if (!Number.isInteger(quantity) || quantity < 1 || quantity > 999) return toast("添加数量必须在 1–999 之间。", true);
  const existing = state.account.profile.inventory.find((entry) => entry.id === pending.itemID)?.quantity || 0;
  const next = existing + quantity;
  if (next > 999) return toast(`当前已有 ${existing} 个，添加后不能超过 999。`, true);
  state.account = await api(`/gm/api/accounts/${state.selectedUIN}/inventory/${pending.itemID}`, { method: "PUT", body: JSON.stringify({ quantity: next }) });
  $("grant-item-dialog").close();
  state.pendingGrantItem = null;
  renderInventory();
  toast(`已给 ${state.account.profile.nickname} 添加 ${pending.name} × ${quantity}。`);
  const summary = state.accounts.find((account) => account.uin === state.selectedUIN);
  if (summary) {
    summary.inventory_count = state.account.profile.inventory.length;
    renderAccounts();
  }
}

function kindLabel(value) {
  return ({ "pet-food":"宠物粮食", "pet-card":"宠物卡", "profile-decoration":"资料装饰", "avatar-cosmetic":"角色装扮", "pet-skill-book":"宠物技能书", "craft-recipe":"合成书", "material":"材料", "forge-gem":"宝石", "inventory-consumable":"消耗品", "account-item":"账号物品", "unknown":"未分类物品" })[value] || value;
}
function escapeHTML(value) { const node = document.createElement("span"); node.textContent = String(value ?? ""); return node.innerHTML; }
function setValue(id, value) { $(id).value = value ?? ""; }
function numberValue(id) { return Number($(id).value); }

function bindEvents() {
  $("account-search").addEventListener("input", renderAccounts);
  $("new-account").addEventListener("click", () => $("create-dialog").showModal());
  $("cancel-create").addEventListener("click", () => $("create-dialog").close());
  bindPasswordToggle("toggle-create-password", "create-password");
  bindPasswordToggle("toggle-profile-password", "profile-password");
  $("create-form").addEventListener("submit", createAccount);
  $("save-profile").addEventListener("click", () => saveProfile().catch(reportError));
  $("delete-account").addEventListener("click", () => deleteAccount().catch(reportError));
  $("refresh-account").addEventListener("click", () => selectAccount(state.selectedUIN).catch(reportError));
  $("profile-degree").addEventListener("change", selectCompetitiveRank);
  $("profile-points").addEventListener("input", deriveCompetitiveRank);
  $("profile-adventure-level").addEventListener("change", selectAdventureRank);
  $("profile-adventure-points").addEventListener("input", deriveAdventureRank);
	$("inventory-kind").addEventListener("change", () => { state.inventoryKind = $("inventory-kind").value; state.inventoryPage = 1; renderInventory(); });
	$("inventory-prev").addEventListener("click", () => { state.inventoryPage--; renderInventory(); });
	$("inventory-next").addEventListener("click", () => { state.inventoryPage++; renderInventory(); });
  $("pet-search").addEventListener("input", renderPetCatalog);
  $("item-search").addEventListener("input", () => { clearTimeout(itemSearchTimer); itemSearchTimer = setTimeout(() => loadItems(true).catch(reportError), 250); });
  $("item-kind").addEventListener("change", () => loadItems(true).catch(reportError));
  $("item-images-only").addEventListener("change", () => loadItems(true).catch(reportError));
  $("item-first").addEventListener("click", () => changeItemPage(1));
  $("item-prev").addEventListener("click", () => changeItemPage(state.itemPage - 1));
  $("item-next").addEventListener("click", () => changeItemPage(state.itemPage + 1));
  $("item-last").addEventListener("click", () => changeItemPage(Math.ceil(state.itemTotal / state.itemPageSize)));
  $("grant-item-form").addEventListener("submit", (event) => confirmGrantItem(event).catch(reportError));
  $("cancel-grant-item").addEventListener("click", () => { state.pendingGrantItem = null; $("grant-item-dialog").close(); });
}

function reportError(error) { console.error(error); toast(error.message || String(error), true); }

async function start() {
  bindEvents();
  await Promise.all([loadHealth(), loadProgression()]);
  await loadItems(true);
  await loadAccounts();
}
start().catch(reportError);
