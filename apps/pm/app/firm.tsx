import React, { useState } from 'react';
import { View, Text, StyleSheet, ScrollView, KeyboardAvoidingView, Platform, Pressable } from 'react-native';
import { Button, Card, Field, apiFromEnv, color, font, space } from '@dwellm8/mobile-shared';
import { useSession } from '../src/auth/session';

/**
 * A verified sign-in is not yet a firm. This is the call that mints one (#31),
 * and until it succeeds there is no organisation for any /v1/ops call to be
 * scoped by — which is why the guard parks a new manager here.
 */
export default function Firm() {
  const { session, refreshFirm, signOut } = useSession();
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const create = async () => {
    setError('');
    setBusy(true);
    try {
      const api = apiFromEnv();
      if (!api) throw new Error('This build has no API configured');
      await api.onboard(name.trim());
      await refreshFirm();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not create the firm');
    } finally {
      setBusy(false);
    }
  };

  return (
    <KeyboardAvoidingView
      style={{ flex: 1, backgroundColor: color.bgTop }}
      behavior={Platform.OS === 'ios' ? 'padding' : 'height'}
    >
      <ScrollView contentContainerStyle={{ paddingTop: space(16), paddingBottom: space(10) }}>
        <View style={s.hero}>
          <Text style={s.wordmark}>Dwellm8</Text>
          <Text style={s.sub}>{session?.email}</Text>
        </View>

        <Card>
          <Text style={s.h}>Name your firm</Text>
          <Text style={s.note}>
            Every property, owner, tenancy and payout you handle sits under this
            firm. A sole manager can use their own name.
          </Text>
          <Field label="Firm name" value={name} onChange={setName} placeholder="Rout Property Services" />
          {error ? <Text style={s.error} accessibilityRole="alert">{error}</Text> : null}
          <Button
            label={busy ? 'Creating…' : 'Create firm'}
            onPress={create}
            disabled={busy || name.trim().length < 2}
          />
          <Pressable onPress={signOut} hitSlop={10} style={{ marginTop: space(4) }}>
            <Text style={s.switch}>Sign out</Text>
          </Pressable>
        </Card>
      </ScrollView>
    </KeyboardAvoidingView>
  );
}

const s = StyleSheet.create({
  hero: { alignItems: 'center', marginBottom: space(8) },
  wordmark: { ...font.h1, fontSize: 32, color: color.inkStrong },
  sub: { ...font.body, color: color.inkSoft, marginTop: space(1) },
  h: { ...font.h3, color: color.inkStrong, marginBottom: space(3) },
  note: { ...font.small, color: color.inkSoft, lineHeight: 19, marginBottom: space(4) },
  error: { ...font.small, color: color.negative, marginBottom: space(3) },
  switch: { ...font.label, color: color.accent, textAlign: 'center' },
});
