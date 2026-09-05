; QQTang 5.2 high-frame-rate timing adapter.
;
; The native phantom (huanying) renderer keeps a 13-entry ring of historic
; player poses.  Its original update routine records one entry per rendered
; frame, so raising limitfps from 30 to 144 compresses all four afterimages
; against the actor.  Keep rendering every frame, but update/decay that ring
; at the original ~30 Hz cadence.  The item-specific lightning/fire overlay
; remains in the native draw loop and therefore retains smooth animation.

BITS 32
ORG 0x01137000

%define IAT_GET_TICK_COUNT       0x00861F04
%define PHANTOM_SAMPLE_MS        33
%define PHANTOM_LAST_SAMPLE      0x138
%define BANANA_SAMPLE_MS         33

section_start:
    db 'QQTFPS04'
    dd phantom_history_hook - section_start
    dd PHANTOM_SAMPLE_MS
    dd banana_status_ctor_hook - section_start
    dd banana_status_update_hook - section_start
    dd BANANA_SAMPLE_MS

    times 0x100-($-$$) db 0

; Client+0x4B3DE, inside the native phantom update/draw routine.  EBP is the
; existing stack frame and [EBP-4] is the phantom object.  Client+0x4B53B is
; the untouched copy-count/draw phase, so skipped samples still render both
; the actor clones and their item-specific overlays on every frame.
phantom_history_hook:
    call dword [IAT_GET_TICK_COUNT]
    mov ecx, [ebp-4]
    mov edx, [ecx+PHANTOM_LAST_SAMPLE]
    test edx, edx
    jz .first_sample
    sub eax, edx
    cmp eax, PHANTOM_SAMPLE_MS
    jb .draw_only

    ; Advance by one fixed interval rather than resetting to this render
    ; frame. Keeping the remainder avoids quantizing 45/60/144/300 FPS to an
    ; integer number of frames per history sample. A long stall resets once
    ; instead of filling the ring with catch-up samples at one position.
    cmp eax, PHANTOM_SAMPLE_MS * 2
    jae .reset_clock
    add edx, PHANTOM_SAMPLE_MS
    mov [ecx+PHANTOM_LAST_SAMPLE], edx
    jmp .native_update
.reset_clock:
    add eax, edx
    mov [ecx+PHANTOM_LAST_SAMPLE], eax
    jmp .native_update
.first_sample:
    mov [ecx+PHANTOM_LAST_SAMPLE], eax

.native_update:
    ; Replay the displaced native instructions and their state branch.
    mov eax, [ebp-4]
    mov ecx, [eax]
    call 0x0040E967
    cmp eax, 4
    je 0x0044B42F
    jmp 0x0044B3ED

.draw_only:
    jmp 0x0044B53B

    times 0x200-($-$$) db 0

; Client+0x1DB4C7, in the native forced-slide status constructor used by the
; banana field item (SceneID 23 -> action 42 -> status type 2).  The status
; object is 12 bytes; native code uses +0 for the vtable and +4 for the actor,
; leaving +8 reserved.  Store the legacy logic sampling interval there.
; Without it, the original update routine compares integer positions on every
; rendered frame and destroys the status whenever a sub-pixel movement has
; not crossed an integer boundary yet.
banana_status_ctor_hook:
    mov dword [esi], 0x007A3494
    mov dword [esi+8], BANANA_SAMPLE_MS
    jmp 0x005DB4CD

    times 0x300-($-$$) db 0

; Client+0x1DB52B, forced-slide status update.  Its argument is the elapsed
; milliseconds already consumed by the client's other timed status objects.
; Compare positions at the original ~30 Hz logic cadence throughout the
; forced slide, not just once at startup.  Each sample reloads the 33 ms
; interval.  Rendering and the actor's native movement update still run on
; every frame; only this integer-position stop test is cadence-limited.
banana_status_update_hook:
    mov eax, [esp+4]
    sub dword [ecx+8], eax
    jg .keep_status
    mov dword [ecx+8], BANANA_SAMPLE_MS

.native_update:
    push esi
    mov esi, [ecx+4]
    test esi, esi
    jmp 0x005DB531

.keep_status:
    ret 4

    times 0x1000-($-$$) db 0
