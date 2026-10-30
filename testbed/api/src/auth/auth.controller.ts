import { BadRequestException, Body, Controller, HttpCode, Post } from '@nestjs/common';
import { AuthService } from './auth.service';

interface Credentials {
  email?: unknown;
  password?: unknown;
}

function parse({ email, password }: Credentials) {
  if (typeof email !== 'string' || !email.includes('@')) throw new BadRequestException('email is required');
  if (typeof password !== 'string' || password.length < 8) throw new BadRequestException('password must be at least 8 characters');
  return { email: email.toLowerCase(), password };
}

@Controller('auth')
export class AuthController {
  constructor(private readonly auth: AuthService) {}

  @Post('signup')
  signup(@Body() body: Credentials) {
    const { email, password } = parse(body);
    return this.auth.signup(email, password);
  }

  @Post('login')
  @HttpCode(200)
  login(@Body() body: Credentials) {
    const { email, password } = parse(body);
    return this.auth.login(email, password);
  }
}
