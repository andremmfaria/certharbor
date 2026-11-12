import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useChangePassword, usePatchProfile, usePatchProfilePreferences, useProfile } from '../api/profile'
import { errorMessage, hasProblemType } from '../api/errors'
import { problemToFieldErrors } from '../api/problemToFieldErrors'
import { ErrorState } from '../components/ErrorState'
import { Skeleton } from '../components/Skeleton'
import { useToast } from '../components/Toast'

interface ProfileForm {
  displayName: string
  email: string
}

interface PasswordForm {
  currentPassword: string
  newPassword: string
}

export function ProfilePage() {
  const profile = useProfile()
  const patchProfile = usePatchProfile()
  const patchPreferences = usePatchProfilePreferences()
  const changePassword = useChangePassword()
  const toast = useToast()

  const {
    register: registerProfile,
    handleSubmit: handleProfileSubmit,
    reset: resetProfile,
    setError: setProfileError,
    formState: { errors: profileErrors },
  } = useForm<ProfileForm>()

  const {
    register: registerPassword,
    handleSubmit: handlePasswordSubmit,
    reset: resetPassword,
    setError: setPasswordError,
    formState: { errors: passwordErrors },
  } = useForm<PasswordForm>()

  const [passwordFormError, setPasswordFormError] = useState<string | undefined>(undefined)

  useEffect(() => {
    if (!profile.data) return
    resetProfile({ displayName: profile.data.displayName, email: profile.data.email })
  }, [profile.data, resetProfile])

  const onSaveProfile = handleProfileSubmit((values) => {
    patchProfile.mutate(
      { body: { displayName: values.displayName, email: values.email } },
      {
        onSuccess: () => toast.success('Profile saved.'),
        onError: (e) => {
          toast.error(errorMessage(e, 'Save failed.'))
          for (const [field, message] of Object.entries(problemToFieldErrors(e as never))) {
            setProfileError(field as keyof ProfileForm, { message })
          }
        },
      },
    )
  })

  const onToggleTheme = () => {
    if (!profile.data) return
    const nextTheme = profile.data.theme === 'dark' ? 'light' : 'dark'
    patchPreferences.mutate(
      { body: { theme: nextTheme } },
      {
        onSuccess: () => toast.success('Preferences saved.'),
        onError: (e) => toast.error(errorMessage(e, 'Save failed.')),
      },
    )
  }

  const onChangePassword = handlePasswordSubmit((values) => {
    setPasswordFormError(undefined)
    changePassword.mutate(
      { body: { currentPassword: values.currentPassword, newPassword: values.newPassword } },
      {
        onSuccess: () => {
          toast.success('Password changed.')
          resetPassword()
        },
        onError: (e) => {
          // A 401/invalid-credentials means the current password was wrong,
          // not that the submitted shape failed validation (422) — these are
          // surfaced distinctly rather than lumped into one generic message.
          if (hasProblemType(e, 'invalid-credentials')) {
            setPasswordFormError('Current password is incorrect.')
            return
          }
          toast.error(errorMessage(e, 'Password change failed.'))
          for (const [field, message] of Object.entries(problemToFieldErrors(e as never))) {
            setPasswordError(field as keyof PasswordForm, { message })
          }
        },
      },
    )
  })

  return (
    <section data-testid="page-profile">
      <div className="page-header">
        <div>
          <div className="breadcrumb">profile</div>
          <h1>Profile</h1>
        </div>
      </div>

      {profile.isPending ? (
        <Skeleton rows={6} label="Loading profile" />
      ) : profile.isError ? (
        <ErrorState problem={profile.error} onRetry={() => void profile.refetch()} />
      ) : (
        <>
          <form onSubmit={onSaveProfile} noValidate className="form-grid" data-testid="profile-details-form">
            <div className="card">
              <div className="card-header">
                <h3>Account details</h3>
              </div>
              <div className="card-body form-grid">
                <div className="form-group">
                  <label htmlFor="profile-display-name">Display name</label>
                  <input id="profile-display-name" type="text" {...registerProfile('displayName')} />
                  {profileErrors.displayName ? (
                    <div className="field-error">{profileErrors.displayName.message}</div>
                  ) : null}
                </div>
                <div className="form-group">
                  <label htmlFor="profile-email">Email</label>
                  <input id="profile-email" type="text" {...registerProfile('email')} />
                  {profileErrors.email ? (
                    <div className="field-error">{profileErrors.email.message}</div>
                  ) : null}
                </div>
              </div>
            </div>
            <div className="page-actions">
              <button className="btn" type="submit" disabled={patchProfile.isPending}>
                {patchProfile.isPending ? 'Saving…' : 'Save details'}
              </button>
            </div>
          </form>

          <div className="card">
            <div className="card-header">
              <h3>Appearance</h3>
            </div>
            <div className="card-body">
              <label className="form-check">
                <input
                  type="checkbox"
                  checked={profile.data?.theme === 'dark'}
                  onChange={onToggleTheme}
                  disabled={patchPreferences.isPending}
                />
                <span>Dark theme</span>
              </label>
            </div>
          </div>

          <form
            onSubmit={onChangePassword}
            noValidate
            className="form-grid"
            data-testid="change-password-form"
          >
            <div className="card">
              <div className="card-header">
                <h3>Change password</h3>
              </div>
              <div className="card-body form-grid">
                <div className="form-group">
                  <label htmlFor="profile-current-password">Current password</label>
                  <input
                    id="profile-current-password"
                    type="password"
                    autoComplete="current-password"
                    {...registerPassword('currentPassword')}
                  />
                  {passwordErrors.currentPassword ? (
                    <div className="field-error">{passwordErrors.currentPassword.message}</div>
                  ) : null}
                  {passwordFormError ? <div className="field-error">{passwordFormError}</div> : null}
                </div>
                <div className="form-group">
                  <label htmlFor="profile-new-password">New password</label>
                  <input
                    id="profile-new-password"
                    type="password"
                    autoComplete="new-password"
                    {...registerPassword('newPassword')}
                  />
                  {passwordErrors.newPassword ? (
                    <div className="field-error">{passwordErrors.newPassword.message}</div>
                  ) : null}
                </div>
              </div>
            </div>
            <div className="page-actions">
              <button className="btn" type="submit" disabled={changePassword.isPending}>
                {changePassword.isPending ? 'Changing…' : 'Change password'}
              </button>
            </div>
          </form>
        </>
      )}
    </section>
  )
}
