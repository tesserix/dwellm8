import React, { useEffect, useMemo, useState } from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView, TextInput } from 'react-native';
import { useRouter } from 'expo-router';
import { profile } from '../src/data/mock';
import {
  AppHeader,
  Card,
  ChevronLeft,
  ChevronRight,
  DocIcon,
  DottedRule,
  GlobeIcon,
  Toast,
  apiFromEnv,
  color,
  font,
  radius,
  space, useBack,
} from '@dwellm8/mobile-shared';

/** The owner's own profile — read from /v1/me and self-served (#240). Demo
 * data renders only when no API is configured. */
function useMe() {
  const api = useMemo(() => apiFromEnv(), []);
  const [state, setState] = useState({
    live: !!api, loading: !!api,
    name: api ? '' : profile.name,
    email: api ? '' : profile.email,
    phone: api ? '' : profile.phone,
  });

  useEffect(() => {
    if (!api) return;
    let alive = true;
    api.me()
      .then((me) => {
        if (alive) setState({ live: true, loading: false, name: me?.display_name ?? '', email: me?.email ?? '', phone: me?.phone ?? '' });
      })
      .catch(() => { if (alive) setState((p) => ({ ...p, loading: false })); });
    return () => { alive = false; };
  }, [api]);

  const save = async (patch: { name?: string; email?: string }) => {
    if (!api) throw new Error('The API is not configured on this build.');
    const me = await api.updateMe({ display_name: patch.name, email: patch.email });
    setState({ live: true, loading: false, name: me.display_name ?? '', email: me.email ?? '', phone: me.phone ?? '' });
  };

  return { ...state, save };
}

export default function Profile() {
  const router = useRouter();
  const goBack = useBack('/(tabs)');
  const me = useMe();
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState<string | null>(null);

  const say = (m: string) => {
    setToast(m);
    setTimeout(() => setToast(null), 2600);
  };

  const save = async () => {
    if (busy) return;
    setBusy(true);
    try {
      await me.save({ name: name.trim(), email: email.trim() });
      setEditing(false);
      say('Saved');
    } catch (err) {
      say((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const initials = (me.name || me.phone || '?')
    .replace(/[^A-Za-z0-9 ]/g, '').trim().split(/\s+/).map((w) => w[0]).join('').slice(0, 2).toUpperCase() || '?';

  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <AppHeader
        title="Profile"
        showCaret={false}
        left={<Pressable onPress={goBack} hitSlop={10}><ChevronLeft size={28} w={2.4} /></Pressable>}
        right={<Pressable hitSlop={10}><Text style={s.logout}>Log Out</Text></Pressable>}
      />
      <ScrollView contentContainerStyle={{ paddingBottom: space(10) }}>
        {toast ? <Toast text={toast} /> : null}
        <View style={s.hero}>
          <View style={s.avatar}><Text style={s.initials}>{initials}</Text></View>
          <Text style={s.name}>{me.name || 'Your portfolio'}</Text>
          {me.email ? <Text style={s.detail}>{me.email}</Text> : null}
          {me.phone ? <Text style={s.detail}>{me.phone}</Text> : null}
        </View>

        {me.live ? (
          <Card>
            {editing ? (
              <>
                <Text style={s.editLabel}>Your name</Text>
                <TextInput style={s.input} value={name} onChangeText={setName} placeholder="Meera Sharma" placeholderTextColor={color.inkFaint} />
                <Text style={s.editLabel}>Email</Text>
                <TextInput style={s.input} value={email} onChangeText={setEmail} placeholder="you@example.in" placeholderTextColor={color.inkFaint} keyboardType="email-address" autoCapitalize="none" />
                <View style={{ flexDirection: 'row', gap: 10, marginTop: space(3) }}>
                  <Pressable style={s.secondary} onPress={() => setEditing(false)}><Text style={s.secondaryText}>Cancel</Text></Pressable>
                  <Pressable style={[s.primary, busy && { opacity: 0.6 }]} onPress={save} disabled={busy}>
                    <Text style={s.primaryText}>{busy ? 'Saving…' : 'Save'}</Text>
                  </Pressable>
                </View>
                <Text style={s.hint}>Your mobile number is verified and never changes here.</Text>
              </>
            ) : (
              <Pressable style={s.row} onPress={() => { setName(me.name); setEmail(me.email); setEditing(true); }}>
                <View style={{ flex: 1 }}>
                  <Text style={s.rowLabel}>{me.name ? 'Your details' : 'Add your name and email'}</Text>
                  <Text style={s.hint}>{me.name ? 'Name and email' : 'Your manager onboarded you — finish your profile'}</Text>
                </View>
                <ChevronRight size={20} c={color.inkFaint} />
              </Pressable>
            )}
          </Card>
        ) : null}

        <Text style={s.section}>Access</Text>
        <Card>
          <Row icon={<GlobeIcon size={22} />} label="People with access" onPress={() => router.push('/access')} />
        </Card>

        <Text style={s.section}>General</Text>
        <Card>
          <Row icon={<GlobeIcon size={22} />} label="Terms and Conditions" />
          <DottedRule />
          <Row icon={<DocIcon size={22} />} label="Help me" />
        </Card>

        <Pressable style={{ marginTop: space(6) }}>
          <Text style={s.delete}>Delete my account</Text>
        </Pressable>
        <Text style={s.version}>Version {profile.version}</Text>
      </ScrollView>
    </View>
  );
}

const Row = ({ icon, label, onPress }: { icon: React.ReactNode; label: string; onPress?: () => void }) => (
  <Pressable style={s.row} onPress={onPress}>
    {icon}
    <Text style={s.rowLabel}>{label}</Text>
    <ChevronRight size={20} c={color.inkFaint} />
  </Pressable>
);

const s = StyleSheet.create({
  logout: { ...font.title, color: color.accent },
  hero: { alignItems: 'center', paddingVertical: space(8) },
  avatar: {
    width: 108, height: 108, borderRadius: 54, backgroundColor: '#7B2E86',
    alignItems: 'center', justifyContent: 'center',
  },
  initials: { fontSize: 38, fontWeight: '800', color: '#FFF' },
  name: { ...font.h1, color: color.inkStrong, marginTop: space(4) },
  detail: { ...font.body, color: color.ink, marginTop: 4 },
  section: { ...font.h2, color: color.inkStrong, marginHorizontal: space(4), marginBottom: space(3), marginTop: space(4) },
  row: { flexDirection: 'row', alignItems: 'center', gap: 14, paddingVertical: space(4) },
  rowLabel: { ...font.body, color: color.inkStrong, flex: 1, fontWeight: '600' },
  editLabel: { ...font.label, color: color.inkSoft, marginTop: space(3), marginBottom: 6 },
  input: {
    ...font.body, color: color.inkStrong, backgroundColor: '#F4F7FA',
    borderRadius: radius.md, paddingHorizontal: space(3), paddingVertical: space(3),
  },
  primary: {
    flex: 1.4, backgroundColor: color.accent, borderRadius: radius.pill,
    paddingVertical: space(3), alignItems: 'center',
  },
  primaryText: { ...font.label, color: '#FFF' },
  secondary: {
    flex: 1, borderWidth: 1.4, borderColor: color.accent, borderRadius: radius.pill,
    paddingVertical: space(3), alignItems: 'center',
  },
  secondaryText: { ...font.label, color: color.accent },
  hint: { ...font.small, color: color.inkSoft, marginTop: space(2), lineHeight: 18 },
  delete: { ...font.h3, color: '#E0524E', textAlign: 'center' },
  version: { ...font.body, color: color.inkSoft, textAlign: 'center', marginTop: space(6) },
});
