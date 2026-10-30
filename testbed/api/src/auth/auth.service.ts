import { ConflictException, Injectable, UnauthorizedException } from '@nestjs/common';
import { JwtService } from '@nestjs/jwt';
import * as bcrypt from 'bcryptjs';
import { DbService } from '../db.service';

@Injectable()
export class AuthService {
  constructor(
    private readonly db: DbService,
    private readonly jwt: JwtService,
  ) {}

  async signup(email: string, password: string) {
    const hash = await bcrypt.hash(password, 10);
    try {
      const { rows } = await this.db.query<{ id: string }>('select app_signup($1, $2) as id', [email, hash]);
      return { id: rows[0].id, email };
    } catch (e: any) {
      if (e.code === '23505') throw new ConflictException('email already registered');
      throw e;
    }
  }

  async login(email: string, password: string) {
    const { rows } = await this.db.query<{ id: string; password_hash: string }>('select * from app_find_login($1)', [email]);
    const user = rows[0];
    if (!user || !(await bcrypt.compare(password, user.password_hash))) {
      throw new UnauthorizedException('invalid credentials');
    }
    return { access_token: await this.jwt.signAsync({ sub: user.id, email }) };
  }
}
