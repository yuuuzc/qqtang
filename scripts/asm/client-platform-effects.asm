; QQTang 5.2 passive function-item visual adapter.
;
; Item 199/467/468 are inventory function items, not wearable platform slots.
; At competitive GAME_BEGIN the Go server sends one reserved 0x1159 record per
; participant. This adapter caches that player-to-effect mask. Mechanical
; lightning consumes the native 0x1159 throw path. Football visuals consume
; the actual contact-kick pair: local REQUEST_MOVE_BOMB 0x0FB4 and remote
; NOTIFY_PLAYER_MOVE_BOMB 0x139C.
; Native gameplay packets and inventory/equipment state remain untouched.

BITS 32
ORG 0x01136000

%define EFFECT_LIGHTNING 0x01
%define EFFECT_SWORD     0x02
%define EFFECT_KICK      0x04

%define RULE_MACHINE     0x01
%define RULE_KICK_BOMB   0x02
%define PROJECTION_MARK  0xFE

%define LOAD_EFFECT      0x00405489
%define DESTROY_EFFECT   0x0040B15E
%define SET_EFFECT_POS   0x0040C900
%define UPDATE_EFFECT    0x0040F376
%define DRAW_EFFECT      0x004053A8
%define FREE_EFFECT      0x0061FE7F

section_start:
    db 'QQTFX010'
    dd remote_action_hook - section_start
    dd boss_bubble_hook - section_start
    dd local_action_hook - section_start
    dd effect_update_hook - section_start
    dd effect_draw_hook - section_start
    dd render_native_action - section_start
    dd cache_projection - section_start
    dd render_effect - section_start
    times 0x100-($-$$) db 0

; Remote PLAYER_THROW_BOMB (0x1159) consumer at Client+0x2007A7. Reserved
; ownership records are consumed here. Native actions render later, after the
; original handler has resolved the action player object.
remote_action_hook:
    cmp dword [esp+4], 0x1159
    jne .original
    mov eax, [esp+8]
    test eax, eax
    jz .original
    cmp byte [eax], PROJECTION_MARK
    jne .original
    cmp byte [eax+1], 'Q'
    jne .original
    cmp byte [eax+2], 'F'
    jne .original
    pushfd
    pushad
    push eax
    call cache_projection
    add esp, 4
    popad
    popfd
    ret 8
.original:
    push ebp
    mov ebp, esp
    sub esp, 0x14
    jmp 0x006007AD

    times 0x180-($-$$) db 0

    times 0x200-($-$$) db 0

; render_native_action(body, actor)
; Native 0x1159 layout: source packed cell @0, destination packed cell @1,
; direction @3, PlayerID @9. Mechanical lightning belongs to the thrown
; bubble's destination. Football contact kicks use render_move_bomb_action.
render_native_action:
    push ebp
    mov ebp, esp
    sub esp, 4
    push ebx
    push esi
    push edi
    mov esi, [ebp+8]
    test esi, esi
    jz .done
    cmp byte [cached_rule], RULE_MACHINE
    jne .done
    movzx eax, byte [cached_player_count]
    test eax, eax
    jz .done
    movzx edx, word [esi+9]
    mov edi, cached_players
.find_player:
    cmp dx, word [edi]
    je .found_player
    add edi, 4
    dec eax
    jnz .find_player
    jmp .done
.found_player:
    movzx eax, byte [edi+2]
    mov [ebp-4], eax

    movzx eax, byte [esi+1]
.decode_position:
    mov edx, eax
    and eax, 0x0F
    imul ebx, eax, 40
    add ebx, 20
    shr edx, 4
    imul edi, edx, 40
    add edi, 20
    add edi, 40
.position_ready:
    test byte [ebp-4], EFFECT_LIGHTNING
    jz .done
    push edi
    push ebx
    push lightning_effect
    call render_effect
    add esp, 12
.done:
    pop edi
    pop esi
    pop ebx
    leave
    ret

    times 0x300-($-$$) db 0

; render_effect(filename, x, y)
; Keep a bounded ring of native effect objects so overlapping actions remain
; visible without leaking one object for the whole match.
render_effect:
    push ebp
    mov ebp, esp
    push ebx
    push esi
    push edi
    mov eax, [effect_ring_index]
    mov edx, eax
    inc edx
    mov [effect_ring_index], edx
    and eax, 31
    lea esi, [effect_ring+eax*4]
    mov edi, [esi]
    test edi, edi
    jz .load
    mov ecx, edi
    call DESTROY_EFFECT
    push edi
    call FREE_EFFECT
    add esp, 4
    mov dword [esi], 0
.load:
    push 0
    push 0
    push dword [ebp+8]
    call LOAD_EFFECT
    add esp, 12
    test eax, eax
    jz .done
    mov [esi], eax
    push dword [ebp+16]
    push dword [ebp+12]
    mov ecx, eax
    call SET_EFFECT_POS
.done:
    pop edi
    pop esi
    pop ebx
    leave
    ret

    times 0x400-($-$$) db 0

sword_effects:
    dd sword_right, sword_up, sword_left, sword_down
kick_effects:
    dd kick_right, kick_up, kick_left, kick_down
effect_ring_index:
    dd 0
effect_ring:
    times 32 dd 0

lightning_effect db 'effect\leidian1.eff', 0
sword_right     db 'effect\siwangliandao_RIGHT.eff', 0
sword_up        db 'effect\siwangliandao_UP.eff', 0
sword_left      db 'effect\siwangliandao_LEFT.eff', 0
sword_down      db 'effect\siwangliandao_DOWN.eff', 0
kick_right      db 'effect\qiquan_RIGHT.eff', 0
kick_up         db 'effect\qiquan_UP.eff', 0
kick_left       db 'effect\qiquan_LEFT.eff', 0
kick_down       db 'effect\qiquan_DOWN.eff', 0

    times 0x590-($-$$) db 0

cached_rule:
    db 0
cached_player_count:
    db 0
    dw 0
cached_players:
    times 8 dd 0                    ; uint16 PlayerID, uint8 mask, padding

    times 0x600-($-$$) db 0

; Remote pirate-Boss bubble appearance fix, already validated by the user.
boss_bubble_hook:
    cmp byte [ebx+0x394], 0x21
    je .pirate
    cmp byte [ebx+0x394], 0x22
    je .pirate
    mov ecx, [edi+4]
    mov eax, [ebx+0x400]
    jmp 0x0060857C
.pirate:
    push byte 0
    push byte 0
    push byte 0x15
    jmp 0x006085AA

    times 0x680-($-$$) db 0

; Local 0x1159 producer at Client+0x219483. The complete native body is the
; current stack local [ebp-0x4c]. Render before the original send calls.
local_action_hook:
    pushfd
    pushad
    lea eax, [ebp-0x4c]
    push esi
    push eax
    call render_native_action
    add esp, 8
    popad
    popfd
    lea eax, [ebp-0x4c]
    mov edi, 0x1159
    jmp 0x0061948B

    times 0x700-($-$$) db 0

; cache_projection(body)
; The common 0x1159 decoder applies its native BYTE/BYTE/BYTE/BYTE/BYTE/
; DWORD/WORD/WORD field conversions before this hook. For our reserved wire
; body that leaves the uint16 PlayerID split across decoded +4 (high byte) and
; +8 (low byte), while the one-byte effect mask lands at decoded +10. Rebuild
; the complete ID: room capacity is eight, but protocol PlayerID is uint16.
cache_projection:
    push ebp
    mov ebp, esp
    push ebx
    push esi
    push edi
    mov esi, [ebp+8]
    mov al, [esi+3]
    mov ah, al
    and al, 0x7F
    cmp al, RULE_MACHINE
    je .valid_rule
    cmp al, RULE_KICK_BOMB
    jne .done
.valid_rule:
    test ah, 0x80
    jz .same_rule
    mov [cached_rule], al
    mov byte [cached_player_count], 0
.same_rule:
    cmp al, [cached_rule]
    jne .done
    xor ebx, ebx
    mov bh, [esi+4]
    mov bl, [esi+8]
    test ebx, ebx
    jz .done
    mov dl, [esi+10]
    cmp al, RULE_MACHINE
    jne .mask_kick
    and dl, EFFECT_LIGHTNING
    jmp .find_slot
.mask_kick:
    and dl, EFFECT_SWORD | EFFECT_KICK
.find_slot:
    movzx ecx, byte [cached_player_count]
    mov edi, cached_players
    test ecx, ecx
    jz .append
.search:
    cmp bx, word [edi]
    je .store
    add edi, 4
    dec ecx
    jnz .search
.append:
    movzx ecx, byte [cached_player_count]
    cmp ecx, 8
    jae .done
    lea edi, [cached_players+ecx*4]
    inc byte [cached_player_count]
.store:
    mov word [edi], bx
    mov byte [edi+2], dl
    mov byte [edi+3], 0
.done:
    pop edi
    pop esi
    pop ebx
    leave
    ret

    times 0x800-($-$$) db 0

; The native player-arrow scene object exists in every active game and owns
; the exact CEffect Update/Draw lifecycle. Reuse those two frame points rather
; than inventing a timer or a second renderer.
effect_update_hook:
    mov eax, [esp+4]
    pushfd
    pushad
    push eax
    call update_effect_ring
    add esp, 4
    popad
    popfd
    push ebx
    push esi
    mov esi, ecx
    push edi
    mov ebx, [esi+8]
    jmp 0x005D2F9E

    times 0x880-($-$$) db 0

effect_draw_hook:
    pushfd
    pushad
    call draw_effect_ring
    popad
    popfd
    push ebp
    mov ebp, esp
    sub esp, 0x18
    jmp 0x005D301D

    times 0x900-($-$$) db 0

update_effect_ring:
    push ebp
    mov ebp, esp
    push ebx
    push esi
    push edi
    mov ebx, 32
    mov edi, effect_ring
.next:
    mov esi, [edi]
    test esi, esi
    jz .advance
    push dword [ebp+8]
    mov ecx, esi
    call UPDATE_EFFECT
    test al, al
    jnz .advance
    mov ecx, esi
    call DESTROY_EFFECT
    push esi
    call FREE_EFFECT
    add esp, 4
    mov dword [edi], 0
.advance:
    add edi, 4
    dec ebx
    jnz .next
    pop edi
    pop esi
    pop ebx
    leave
    ret

    times 0x980-($-$$) db 0

draw_effect_ring:
    push ebx
    push esi
    mov ebx, 32
    mov esi, effect_ring
.next:
    mov ecx, [esi]
    test ecx, ecx
    jz .advance
    call DRAW_EFFECT
.advance:
    add esi, 4
    dec ebx
    jnz .next
    pop esi
    pop ebx
    ret

    times 0xA00-($-$$) db 0

; Local mechanical multi-bubble sender. FUN_005ff40c/FUN_005ff58e/
; FUN_005ff712 each call FUN_005ffe30 once per generated bubble (up to four).
; At Client+0x1FFE86 EDI is the complete native 0x1159 body and EAX is the
; already-resolved acting player. Keep this hook inside the loop so every
; generated bubble receives its own lightning effect.
local_multi_bubble_hook:
    pushfd
    pushad
    push eax
    push edi
    call render_native_action
    add esp, 8
    popad
    popfd
    mov esi, 0x1159
    jmp 0x005FFE8B

    times 0xA80-($-$$) db 0

; Remote native 0x1159 consumer after it has resolved the action player.
; ESI is the decoded body and EBX is the resolved player object. This is the
; observer-side counterpart of local_multi_bubble_hook.
remote_multi_bubble_hook:
    pushfd
    pushad
    push ebx
    push esi
    call render_native_action
    add esp, 8
    popad
    popfd
    mov eax, [ebx+0x400]
    jmp 0x00600862

    times 0xB00-($-$$) db 0

; Local rule-2 contact kick. Client+0x20D1A6 has completed the exact 20-byte
; REQUEST_MOVE_BOMB body at [EBP-0x38]; EDI is still the acting player until
; the following native instruction overwrites it with schema 0x0FB4.
local_move_bomb_hook:
    pushfd
    pushad
    lea eax, [ebp-0x38]
    push eax
    call render_move_bomb_action
    add esp, 4
    popad
    popfd
    lea eax, [ebp-0x38]
    mov edi, 0x0FB4
    jmp 0x0060D1AE

    times 0xB80-($-$$) db 0

; Remote NOTIFY_PLAYER_MOVE_BOMB consumer. Render from its decoded 20-byte
; body, then replay the untouched native prologue.
remote_move_bomb_hook:
    cmp dword [esp+4], 0x139C
    jne .original
    mov eax, [esp+8]
    test eax, eax
    jz .original
    pushfd
    pushad
    push eax
    call render_move_bomb_action
    add esp, 4
    popad
    popfd
.original:
    push ebp
    mov ebp, esp
    sub esp, 0x14
    jmp 0x005FD252

    times 0xC00-($-$$) db 0

; render_move_bomb_action(body)
; Both 0x0FB4 and 0x139C use the same decoded layout:
;   PlayerID +0, source Row/Col +4/+6, destination +8/+10,
;   direction +12, BombID +13, BombProp +15, trailing flag +19.
render_move_bomb_action:
    push ebp
    mov ebp, esp
    sub esp, 4
    push ebx
    push esi
    push edi
    cmp byte [cached_rule], RULE_KICK_BOMB
    jne .done
    mov esi, [ebp+8]
    test esi, esi
    jz .done
    movzx eax, byte [cached_player_count]
    test eax, eax
    jz .done
    movzx edx, word [esi]
    mov edi, cached_players
.find_player:
    cmp dx, word [edi]
    je .found_player
    add edi, 4
    dec eax
    jnz .find_player
    jmp .done
.found_player:
    movzx eax, byte [edi+2]
    mov [ebp-4], eax
    ; Native movement proves direction 0 (RIGHT) increments Col and direction
    ; 1 (UP) decrements Row. Screen X is therefore Col; screen Y is Row.
    movsx eax, word [esi+6]
    imul ebx, eax, 40
    add ebx, 20
    movsx eax, word [esi+4]
    imul edi, eax, 40
    add edi, 20
    movzx ecx, byte [esi+12]
    and ecx, 3
    test byte [ebp-4], EFFECT_SWORD
    jz .kick_effect
    mov eax, [sword_effects+ecx*4]
    push edi
    push ebx
    push eax
    call render_effect
    add esp, 12
.kick_effect:
    test byte [ebp-4], EFFECT_KICK
    jz .done
    movzx ecx, byte [esi+12]
    and ecx, 3
    mov eax, [kick_effects+ecx*4]
    push edi
    push ebx
    push eax
    call render_effect
    add esp, 12
.done:
    pop edi
    pop esi
    pop ebx
    leave
    ret

    times 0x1000-($-$$) db 0
