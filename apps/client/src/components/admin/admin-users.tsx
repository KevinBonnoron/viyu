import { createColumnHelper, tableFeatures, useTable } from '@tanstack/react-table';
import { Pencil, Trash2 } from 'lucide-react';
import { type FormEvent, useCallback, useEffect, useState } from 'react';
import { ConfirmDialog } from '@/components/confirm-dialog';
import { ErrorSnackbar } from '@/components/error-snackbar';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { adminApi } from '@/lib/admin-api';
import { ApiError } from '@/lib/api-error';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import type { AdminUser, AdminUsersPage } from '@/types';
import { adminErrorKey } from './admin-error';
import { AdminUserForm } from './admin-user-form';

export function AdminUsers({ currentUserId, retryAdmin }: { currentUserId: string; retryAdmin: () => void }) {
  const { t } = useI18n();
  const [search, setSearch] = useState('');
  const [query, setQuery] = useState('');
  const [page, setPage] = useState(1);
  const [result, setResult] = useState<AdminUsersPage>();
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<TranslationKey>();
  const [revision, setRevision] = useState(0);
  const [forbidden, setForbidden] = useState(false);
  const [editor, setEditor] = useState<{ user?: AdminUser }>();
  const [deleting, setDeleting] = useState<AdminUser>();
  const onForbidden = useCallback(() => setForbidden(true), []);

  useEffect(() => {
    if (forbidden) {
      return;
    }
    const controller = new AbortController();
    setLoading(true);
    setLoadError(undefined);
    void adminApi
      .listUsers(query, page, { signal: controller.signal })
      .then((data) => {
        if (controller.signal.aborted) {
          return;
        }
        if (page > Math.max(1, data.totalPages)) {
          setPage(Math.max(1, data.totalPages));
          return;
        }
        setResult(data);
        setLoading(false);
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) {
          return;
        }
        setLoading(false);
        if (error instanceof ApiError && error.status === 403) {
          onForbidden();
        } else {
          setLoadError(adminErrorKey(error));
        }
      });
    return () => controller.abort();
  }, [query, page, revision, forbidden, onForbidden]);

  const submitSearch = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setQuery(search.trim());
    setPage(1);
    setRevision((value) => value + 1);
  };

  const remove = async (user: AdminUser) => {
    try {
      await adminApi.deleteUser(user.id);
      setRevision((value) => value + 1);
    } catch (error) {
      if (error instanceof ApiError && error.status === 403) {
        onForbidden();
        return;
      }
      throw error;
    }
  };

  if (forbidden) {
    return (
      <section role="alert" className="space-y-3 rounded-2xl border bg-card p-5">
        <h2 className="font-semibold">{t('admin.forbidden')}</h2>
        <p className="text-sm text-muted-foreground">{t('admin.forbiddenDescription')}</p>
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            setForbidden(false);
            retryAdmin();
          }}
        >
          {t('admin.retry')}
        </Button>
      </section>
    );
  }

  return (
    <div className="space-y-6">
      <section className="space-y-4 rounded-2xl border bg-card p-5" aria-labelledby="admin-users-title">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h2 id="admin-users-title" className="text-lg font-semibold">
            {t('admin.users')}
          </h2>
          <Button type="button" disabled={!!editor || !!deleting} onClick={() => setEditor({})}>
            {t('admin.createUser')}
          </Button>
        </div>
        <form onSubmit={submitSearch} className="flex flex-wrap gap-2">
          <label htmlFor="admin-user-search" className="sr-only">
            {t('admin.searchLabel')}
          </label>
          <Input
            id="admin-user-search"
            type="search"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('admin.searchPlaceholder')}
            className="min-w-0 flex-1"
          />
          <Button type="submit" variant="outline">
            {t('admin.search')}
          </Button>
        </form>
        {loading ? (
          <output className="block py-3 text-sm text-muted-foreground">{t('admin.loadingUsers')}</output>
        ) : loadError ? (
          <ErrorSnackbar
            message={`${t('admin.usersLoadError')} ${t(loadError)}`}
            action={t('admin.retry')}
            onAction={() => setRevision((value) => value + 1)}
          />
        ) : (
          result && (
            <>
              {result.items.length === 0 ? (
                <p className="py-3 text-sm text-muted-foreground">{t(query ? 'admin.noMatches' : 'admin.noUsers')}</p>
              ) : (
                <AdminUsersTable
                  users={result.items}
                  currentUserId={currentUserId}
                  editor={!!editor}
                  deleting={!!deleting}
                  onEdit={(user) => setEditor({ user })}
                  onDelete={setDeleting}
                />
              )}
              <nav aria-label={t('admin.pagination')} className="flex flex-wrap items-center justify-between gap-3 border-t pt-4">
                <p className="text-sm text-muted-foreground">
                  {t('admin.pageSummary', { page: result.page, pages: Math.max(1, result.totalPages), total: result.totalItems })}
                </p>
                <div className="flex gap-2">
                  <Button type="button" size="sm" variant="outline" disabled={page <= 1} onClick={() => setPage((value) => value - 1)}>
                    {t('admin.previous')}
                  </Button>
                  <Button type="button" size="sm" variant="outline" disabled={page >= result.totalPages} onClick={() => setPage((value) => value + 1)}>
                    {t('admin.next')}
                  </Button>
                </div>
              </nav>
            </>
          )
        )}
      </section>
      {deleting && (
        <ConfirmDialog
          title={t('admin.deleteUser')}
          description={t('admin.deleteUserConfirm', { email: deleting.email })}
          confirmLabel={t('admin.confirmDelete')}
          pendingLabel={t('admin.deleting')}
          cancelLabel={t('admin.cancel')}
          errorMessage={(cause) => t(adminErrorKey(cause))}
          onConfirm={() => remove(deleting)}
          onClose={() => setDeleting(undefined)}
        />
      )}
      {editor && (
        <AdminUserForm
          key={editor.user?.id ?? 'create'}
          user={editor.user}
          currentUserId={currentUserId}
          onForbidden={onForbidden}
          onCancel={() => setEditor(undefined)}
          onSaved={() => {
            setEditor(undefined);
            setRevision((value) => value + 1);
          }}
        />
      )}
    </div>
  );
}

const features = tableFeatures({});
const userColumn = createColumnHelper<typeof features, AdminUser>();

function AdminUsersTable({
  users,
  currentUserId,
  editor,
  deleting,
  onEdit,
  onDelete,
}: {
  users: AdminUser[];
  currentUserId: string;
  editor: boolean;
  deleting: boolean;
  onEdit: (user: AdminUser) => void;
  onDelete: (user: AdminUser) => void;
}) {
  const { t } = useI18n();
  const columns = userColumn.columns([
    userColumn.accessor((user) => user.name || user.email, {
      id: 'user',
      header: () => t('admin.userColumn'),
      cell: ({ row }) => (
        <div className="min-w-0">
          <p className="truncate font-medium">
            {row.original.name || row.original.email}
            {row.original.id === currentUserId && <span className="ml-2 font-normal text-muted-foreground">{t('admin.you')}</span>}
          </p>
          <p className="truncate text-xs text-muted-foreground">{row.original.email}</p>
        </div>
      ),
    }),
    userColumn.accessor('role', {
      header: () => t('admin.role'),
      cell: ({ getValue }) => (
        <span
          className={`inline-flex rounded-full px-2 py-1 text-xs font-medium ${getValue() === 'admin' ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground'}`}
        >
          {t(getValue() === 'admin' ? 'admin.roleAdmin' : 'admin.roleUser')}
        </span>
      ),
    }),
    userColumn.accessor('verified', {
      header: () => t('admin.status'),
      cell: ({ getValue }) => <span className="text-sm text-muted-foreground">{t(getValue() ? 'admin.verified' : 'admin.unverified')}</span>,
    }),
    userColumn.display({
      id: 'actions',
      header: () => <span className="sr-only">{t('admin.actions')}</span>,
      cell: ({ row }) => {
        const user = row.original;
        return (
          <div className="flex items-center justify-end gap-1">
            <Button
              type="button"
              size="icon"
              variant="ghost"
              disabled={editor || deleting}
              aria-label={t('admin.editUserNamed', { name: user.name || user.email })}
              title={t('admin.edit')}
              onClick={() => onEdit(user)}
              className="size-8"
            >
              <Pencil aria-hidden="true" className="size-4" />
            </Button>
            <Button
              type="button"
              size="icon"
              variant="ghost"
              disabled={editor || deleting || user.id === currentUserId}
              title={user.id === currentUserId ? t('admin.selfProtection') : t('admin.delete')}
              aria-label={t('admin.deleteUserNamed', { name: user.name || user.email })}
              onClick={() => onDelete(user)}
              className="size-8 text-destructive hover:text-destructive"
            >
              <Trash2 aria-hidden="true" className="size-4" />
            </Button>
          </div>
        );
      },
    }),
  ]);
  const table = useTable({ features, columns, data: users, getRowId: (user) => user.id });
  return (
    <div className="rounded-lg border">
      <table className="w-full table-fixed text-left text-sm">
        <colgroup>
          <col />
          <col className="w-28 sm:w-36" />
          <col className="w-32 sm:w-56" />
          <col className="w-32" />
        </colgroup>
        <thead className="bg-muted/40 text-xs font-medium text-muted-foreground">
          {table.getHeaderGroups().map((group) => (
            <tr key={group.id}>
              {group.headers.map((header) => (
                <th key={header.id} scope="col" className="px-4 py-3 font-medium">
                  {header.isPlaceholder ? null : <table.FlexRender header={header} />}
                </th>
              ))}
            </tr>
          ))}
        </thead>
        <tbody className="divide-y bg-card">
          {table.getRowModel().rows.map((row) => (
            <tr key={row.id} className="hover:bg-muted/30">
              {row.getAllCells().map((cell) => (
                <td key={cell.id} className="px-4 py-3">
                  <table.FlexRender cell={cell} />
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
