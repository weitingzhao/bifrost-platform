const ROLE_CLASS: Record<string, string> = {
  viewer: 'platform-auth-badge--viewer',
  operator: 'platform-auth-badge--operator',
  admin: 'platform-auth-badge--admin',
}

export function platformAuthRoleBadgeClass(role: string): string {
  return ROLE_CLASS[role.toLowerCase()] ?? ROLE_CLASS.viewer
}

export function platformAuthAuthenticatedBadgeClass(): string {
  return 'platform-auth-badge--authenticated'
}
