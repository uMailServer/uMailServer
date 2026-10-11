import { it, expect } from 'vitest'
import { passwordError, emailError, domainError } from './validation'

it('R173 password policy mirrors server', () => {
  expect(passwordError('short1!A')).toBeNull()
  expect(passwordError('Sh1!')).toMatch(/8/)
  expect(passwordError('alllower1!')).toMatch(/uppercase/)
  expect(passwordError('ALLUPPER1!')).toMatch(/lowercase/)
  expect(passwordError('NoDigits!!')).toMatch(/digit/)
  expect(passwordError('NoSpecial12')).toMatch(/special/)
  expect(passwordError('Aa1!' + 'x'.repeat(125))).toMatch(/128/)
})
it('R173 email/domain format mirrors server', () => {
  expect(emailError('a@b.com')).toBeNull()
  expect(emailError('a@@b.com')).toMatch(/@/)
  expect(emailError('a/b@x.com')).not.toBeNull()
  expect(emailError('@x.com')).not.toBeNull()
  expect(domainError('example.com')).toBeNull()
  expect(domainError('-bad.com')).not.toBeNull()
  expect(domainError('a..com')).not.toBeNull()
  expect(domainError('a b.com')).not.toBeNull()
})
