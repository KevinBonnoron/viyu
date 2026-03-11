/** The label of the modifier key of a keyboard shortcut (the one the app binds with `ctrlKey || metaKey`) on a platform name. */
export function modifierLabelFor(platform: string): string {
  return /mac|iphone|ipad|ipod/i.test(platform) ? '⌘' : 'Ctrl';
}

/** The modifier label of this device: ⌘ on macOS and iOS, Ctrl elsewhere. */
export function shortcutModifierLabel(): string {
  const { userAgentData, platform } = navigator as Navigator & { userAgentData?: { platform?: string } };
  return modifierLabelFor(userAgentData?.platform ?? platform ?? '');
}
