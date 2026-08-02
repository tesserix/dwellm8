import React, { useState } from 'react';
import { View, Text, StyleSheet, Pressable, ScrollView, TextInput } from 'react-native';
import { useRouter } from 'expo-router';
import { AppHeader, Card, ChevronLeft, ChevronRight, DocIcon, DottedRule, GlobeIcon, Toast, color, font, radius, space } from '@dwellm8/mobile-shared';
import { useLiveData, useMe } from '../src/data/source';

export default function Profile() {
  const router = useRouter();
  const { tenancy } = useLiveData();
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

  const startEdit = () => {
    setName(me.name);
    setEmail(me.email);
    setEditing(true);
  };

  const save = async () => {
    if (busy) return;
    setBusy(true);
    try {
      await me.save({ name: name.trim(), email: email.trim() });
      setEditing(false);
      say('Saved — this is how your manager sees you now');
    } catch (err) {
      say((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const title = me.name || tenancy.unit || 'Your tenancy';
  const initials = (me.name || tenancy.unit || me.phone || '?')
    .replace(/[^A-Za-z0-9 ]/g, '').trim().split(/\s+/).map((w) => w[0]).join('').slice(0, 2).toUpperCase() || '?';

  return (
    <View style={{ flex: 1, backgroundColor: color.bgTop }}>
      <AppHeader
        title="Profile"
        showCaret={false}
        left={<Pressable onPress={() => router.back()} hitSlop={10}><ChevronLeft size={28} w={2.4} /></Pressable>}
        right={<Pressable hitSlop={10}><Text style={s.logout}>Log Out</Text></Pressable>}
      />
      <ScrollView contentContainerStyle={{ paddingBottom: space(10) }}>
        {toast ? <Toast text={toast} /> : null}
        <View style={s.hero}>
          <View style={s.avatar}><Text style={s.initials}>{initials}</Text></View>
          <Text style={s.name}>{title}</Text>
          {me.email ? <Text style={s.detail}>{me.email}</Text> : null}
          {me.phone ? <Text style={s.detail}>{me.phone}</Text> : null}
        </View>

        <Card>
          {editing ? (
            <>
              <Text style={s.editLabel}>Your name</Text>
              <TextInput style={s.input} value={name} onChangeText={setName} placeholder="Ananya Verma" placeholderTextColor={color.inkFaint} />
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
            <Pressable style={s.row} onPress={startEdit}>
              <View style={{ flex: 1 }}>
                <Text style={s.rowLabel}>{me.name ? 'Your details' : 'Add your name and email'}</Text>
                <Text style={s.hint}>{me.name ? 'Name and email' : 'So your manager knows who they are talking to'}</Text>
              </View>
              <ChevronRight size={20} c={color.inkFaint} />
            </Pressable>
          )}
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
        <Text style={s.version}>Version 0.1.0 (1)</Text>
      </ScrollView>
    </View>
  );
}

const Row = ({ icon, label }: { icon: React.ReactNode; label: string }) => (
  <Pressable style={s.row}>
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
  name: { ...font.h1, color: color.inkStrong, marginTop: space(4), textAlign: 'center', marginHorizontal: space(4) },
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
