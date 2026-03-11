export type AdminRole = 'user' | 'admin';

export interface AdminUser {
  id: string;
  name: string;
  email: string;
  role: AdminRole;
  verified: boolean;
  created: string;
}

export interface AdminUsersPage {
  items: AdminUser[];
  page: number;
  totalPages: number;
  totalItems: number;
}

export interface AdminUserInput {
  name: string;
  email: string;
  password: string;
  passwordConfirm: string;
  role: AdminRole;
}
